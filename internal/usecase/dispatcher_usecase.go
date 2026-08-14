package usecase

import (
	"context"
	"time"

	"mailpulse/internal/entity"
	"mailpulse/internal/repository"
	"mailpulse/internal/usecase/event"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// DispatcherUseCase drains event_runs. It claims due rows with SKIP LOCKED so
// several workers share the queue, executes the handler from the registry, and
// only then writes the next occurrence of a recurring event.
type DispatcherUseCase struct {
	DB         *gorm.DB
	Log        *logrus.Logger
	Runs       *repository.EventRunRepository
	Deliveries *repository.NotificationDeliveryRepository
	Matches    *repository.MatchedEmailRepository
	Watchers   *repository.WatcherRepository
	Events     *repository.WatcherEventRepository
	Handlers   *event.Registry
}

func NewDispatcherUseCase(db *gorm.DB, log *logrus.Logger,
	runs *repository.EventRunRepository, deliveries *repository.NotificationDeliveryRepository,
	matches *repository.MatchedEmailRepository, watchers *repository.WatcherRepository,
	events *repository.WatcherEventRepository, handlers *event.Registry) *DispatcherUseCase {
	return &DispatcherUseCase{
		DB: db, Log: log, Runs: runs, Deliveries: deliveries,
		Matches: matches, Watchers: watchers, Events: events, Handlers: handlers,
	}
}

// Tick processes one batch of due runs and reports how many it handled.
func (c *DispatcherUseCase) Tick(ctx context.Context, limit int) (int, error) {
	db := c.DB.WithContext(ctx)

	var claimed []entity.EventRun
	err := db.Transaction(func(tx *gorm.DB) error {
		runs, err := c.Runs.ClaimDue(tx, time.Now().UnixMilli(), limit)
		if err != nil {
			return err
		}
		claimed = runs

		// flip to running inside the claim so no other worker can take them.
		// attempt is deliberately not bumped here: execute() increments the
		// struct it holds, and its save would otherwise write the stale value
		// straight back over a SQL-side increment.
		for i := range claimed {
			now := time.Now().UnixMilli()
			if err := tx.Model(&entity.EventRun{}).Where("id = ?", claimed[i].ID).
				Updates(map[string]any{
					"status":     entity.RunStatusRunning,
					"started_at": now,
				}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	for i := range claimed {
		c.execute(ctx, &claimed[i])
	}

	return len(claimed), nil
}

// Execute runs a single run immediately, which is what _retry and the event
// test endpoint both need.
func (c *DispatcherUseCase) Execute(ctx context.Context, run *entity.EventRun) {
	c.execute(ctx, run)
}

func (c *DispatcherUseCase) execute(ctx context.Context, run *entity.EventRun) {
	db := c.DB.WithContext(ctx)

	// counted here so both paths agree: the queue tick and the direct calls
	// from _retry and the event test endpoint
	run.Attempt++
	run.Status = entity.RunStatusRunning
	run.StartedAt = ptr(time.Now().UnixMilli())

	match := new(entity.MatchedEmail)
	if err := c.Matches.FindById(db, match, run.MatchedEmailID); err != nil {
		c.fail(db, run, "matched email is gone: "+err.Error())
		return
	}

	watcherEvent := new(entity.WatcherEvent)
	if err := c.Events.FindById(db, watcherEvent, run.WatcherEventID); err != nil {
		c.fail(db, run, "watcher event is gone: "+err.Error())
		return
	}

	watcher := new(entity.Watcher)
	if err := c.Watchers.FindById(db, watcher, watcherEvent.WatcherID); err != nil {
		c.fail(db, run, "watcher is gone: "+err.Error())
		return
	}

	handler, err := c.Handlers.Get(watcherEvent.Type)
	if err != nil {
		c.fail(db, run, err.Error())
		return
	}

	notifiers, err := c.Events.FindNotifiers(db, watcherEvent.ID)
	if err != nil {
		c.fail(db, run, "could not load notifiers: "+err.Error())
		return
	}

	output, execErr := handler.Execute(ctx, event.Input{
		Occurrence: run.Occurrence,
		Watcher:    watcher,
		Event:      watcherEvent,
		Email:      match,
		Config:     []byte(entity.JSONOrEmpty(run.ConfigSnapshot, "{}")),
		Notifiers:  notifiers,
	})

	c.writeDeliveries(db, run, output.Deliveries)

	now := time.Now().UnixMilli()
	run.FinishedAt = &now
	if len(output.Result) > 0 {
		run.Result = entity.JSON(output.Result)
	}

	if execErr != nil {
		run.Error = ptr(execErr.Error())

		// retry within the same occurrence until max_attempts, then give up
		if run.Attempt < run.MaxAttempts {
			backoff := time.Duration(1<<uint(run.Attempt)) * 30 * time.Second
			retryAt := time.Now().Add(backoff).UnixMilli()
			run.Status = entity.RunStatusPending
			run.ScheduledAt = retryAt
			run.NextRetryAt = &retryAt
			run.FinishedAt = nil
			c.save(db, run)
			return
		}

		run.Status = entity.RunStatusFailed
		c.save(db, run)
		return
	}

	run.Status = entity.RunStatusSucceeded
	run.Error = nil
	c.save(db, run)

	c.scheduleNext(db, run, watcherEvent)
}

// scheduleNext writes occurrence n+1 only after n succeeded, so the repeat
// chain can never run ahead of itself or survive a cancel.
func (c *DispatcherUseCase) scheduleNext(db *gorm.DB, run *entity.EventRun, watcherEvent *entity.WatcherEvent) {
	if watcherEvent.RunMode != entity.RunModeRecurring {
		return
	}

	if watcherEvent.RepeatMax != nil && run.Occurrence >= *watcherEvent.RepeatMax {
		return
	}

	if run.AcknowledgedAt != nil && watcherEvent.StopOnAck {
		return
	}

	interval := 0
	if watcherEvent.RepeatIntervalSeconds != nil {
		interval = *watcherEvent.RepeatIntervalSeconds
	}
	if interval <= 0 {
		// a cron expression needs a scheduler library to expand; until one is
		// wired, a recurring event without an interval simply stops here
		c.Log.Warnf("Event %s is recurring by cron, which is not expanded yet", watcherEvent.ID)
		return
	}

	next := time.Now().Add(time.Duration(interval) * time.Second)
	if watcherEvent.RepeatUntil != nil && next.UnixMilli() > *watcherEvent.RepeatUntil {
		return
	}

	follow := &entity.EventRun{
		ID:             uuid.NewString(),
		UserID:         run.UserID,
		MatchedEmailID: run.MatchedEmailID,
		WatcherEventID: run.WatcherEventID,
		Occurrence:     run.Occurrence + 1,
		Status:         entity.RunStatusPending,
		ScheduledAt:    next.UnixMilli(),
		MaxAttempts:    run.MaxAttempts,
		ConfigSnapshot: run.ConfigSnapshot,
	}

	if err := c.Runs.Create(db, follow); err != nil {
		// the unique (match, event, occurrence) index makes a duplicate here
		// harmless rather than a double notification
		c.Log.WithError(err).Warnf("Failed to schedule occurrence %d", follow.Occurrence)
	}
}

func (c *DispatcherUseCase) writeDeliveries(db *gorm.DB, run *entity.EventRun, deliveries []event.Delivery) {
	for _, item := range deliveries {
		record := &entity.NotificationDelivery{
			ID:              uuid.NewString(),
			EventRunID:      run.ID,
			NotifierID:      item.NotifierID,
			ChannelType:     item.ChannelType,
			Status:          item.Status,
			RenderedMessage: nilIfEmpty(item.RenderedMessage),
		}

		if item.ProviderMessageID != "" {
			record.ProviderMessageID = &item.ProviderMessageID
		}
		if item.Error != "" {
			record.Error = &item.Error
		}
		if item.Status == entity.DeliveryStatusSent {
			record.SentAt = ptr(time.Now().UnixMilli())
		}

		if err := c.Deliveries.Create(db, record); err != nil {
			c.Log.WithError(err).Warn("Failed to record a delivery")
		}
	}
}

func (c *DispatcherUseCase) fail(db *gorm.DB, run *entity.EventRun, reason string) {
	now := time.Now().UnixMilli()
	run.Status = entity.RunStatusFailed
	run.FinishedAt = &now
	run.Error = &reason
	c.save(db, run)
}

func (c *DispatcherUseCase) save(db *gorm.DB, run *entity.EventRun) {
	if err := c.Runs.Update(db, run); err != nil {
		c.Log.WithError(err).Warnf("Failed to save run %s", run.ID)
	}
}
