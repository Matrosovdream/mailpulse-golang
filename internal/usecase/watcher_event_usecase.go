package usecase

import (
	"context"
	"time"

	"mailpulse/internal/entity"
	"mailpulse/internal/model"
	"mailpulse/internal/model/converter"
	"mailpulse/internal/repository"
	"mailpulse/internal/usecase/event"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type WatcherEventUseCase struct {
	DB         *gorm.DB
	Log        *logrus.Logger
	Validate   *validator.Validate
	Watchers   *repository.WatcherRepository
	Events     *repository.WatcherEventRepository
	Notifiers  *repository.NotifierRepository
	Matches    *repository.MatchedEmailRepository
	Runs       *repository.EventRunRepository
	Handlers   *event.Registry
	Dispatcher *DispatcherUseCase
	Audit      *AuditUseCase
}

func NewWatcherEventUseCase(db *gorm.DB, log *logrus.Logger, validate *validator.Validate,
	watchers *repository.WatcherRepository, events *repository.WatcherEventRepository,
	notifiers *repository.NotifierRepository, matches *repository.MatchedEmailRepository,
	runs *repository.EventRunRepository, handlers *event.Registry, audit *AuditUseCase) *WatcherEventUseCase {
	return &WatcherEventUseCase{
		DB: db, Log: log, Validate: validate,
		Watchers: watchers, Events: events, Notifiers: notifiers,
		Matches: matches, Runs: runs, Handlers: handlers, Audit: audit,
	}
}

// SetDispatcher closes the loop between this usecase and the dispatcher, which
// needs the event registry this usecase also holds.
func (c *WatcherEventUseCase) SetDispatcher(dispatcher *DispatcherUseCase) {
	c.Dispatcher = dispatcher
}

func (c *WatcherEventUseCase) ownedWatcher(db *gorm.DB, watcherID, userID string) (*entity.Watcher, error) {
	watcher := new(entity.Watcher)
	if err := c.Watchers.FindByIdAndUser(db, watcher, watcherID, userID); err != nil {
		return nil, fiber.NewError(fiber.StatusNotFound, "watcher not found")
	}
	return watcher, nil
}

func (c *WatcherEventUseCase) Create(ctx context.Context, request *model.CreateWatcherEventRequest) (*model.WatcherEventResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		c.Log.Warnf("Invalid event request : %+v", err)
		return nil, fiber.ErrBadRequest
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	if _, err := c.ownedWatcher(tx, request.WatcherID, request.UserID); err != nil {
		return nil, err
	}

	created, err := c.createWithin(tx, request)
	if err != nil {
		return nil, err
	}

	c.Audit.Record(ctx, tx, AuditEntry{ActorID: &request.UserID, Action: "watcher_event.created",
		EntityType: "watcher_events", EntityID: &created.ID})

	if err := tx.Commit().Error; err != nil {
		return nil, fiber.ErrInternalServerError
	}

	return c.load(c.DB.WithContext(ctx), created)
}

// createWithin is shared by the standalone endpoint and the inline events on
// watcher creation, so both validate identically.
func (c *WatcherEventUseCase) createWithin(tx *gorm.DB, request *model.CreateWatcherEventRequest) (*entity.WatcherEvent, error) {
	handler, err := c.Handlers.Get(request.Type)
	if err != nil {
		return nil, err
	}

	if err := handler.Validate(request.Config); err != nil {
		return nil, err
	}

	runMode := request.RunMode
	if runMode == "" {
		runMode = entity.RunModeImmediate
	}

	// mirrors ck_watcher_events_recurring, but returns a message that says
	// what to do rather than naming a constraint
	if runMode == entity.RunModeRecurring && request.RepeatIntervalSeconds == nil && request.CronExpression == "" {
		return nil, fiber.NewError(fiber.StatusBadRequest,
			"a recurring event needs either repeat_interval_seconds or a cron_expression")
	}

	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}

	watcherEvent := &entity.WatcherEvent{
		ID:                    uuid.NewString(),
		WatcherID:             request.WatcherID,
		Type:                  request.Type,
		Config:                jsonOrDefault(request.Config, "{}"),
		Position:              request.Position,
		Enabled:               enabled,
		RunMode:               runMode,
		DelaySeconds:          request.DelaySeconds,
		RepeatIntervalSeconds: request.RepeatIntervalSeconds,
		RepeatMax:             request.RepeatMax,
		RepeatUntil:           request.RepeatUntil,
		CronExpression:        nilIfEmpty(request.CronExpression),
		StopOnAck:             request.StopOnAck,
	}

	if err := c.Events.Create(tx, watcherEvent); err != nil {
		c.Log.Warnf("Failed create watcher event : %+v", err)
		return nil, fiber.ErrInternalServerError
	}

	if handler.UsesNotifiers() {
		if err := c.attachNotifiers(tx, watcherEvent.ID, request.UserID, request.NotifierIDs); err != nil {
			return nil, err
		}
	}

	return watcherEvent, nil
}

// attachNotifiers refuses ids the caller does not own or that are unverified,
// so an event can never be built that would silently fail to deliver.
func (c *WatcherEventUseCase) attachNotifiers(tx *gorm.DB, eventID, userID string, ids []string) error {
	if len(ids) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "this event type needs at least one notifier")
	}

	notifiers, err := c.Notifiers.FindByIds(tx, userID, ids)
	if err != nil {
		return fiber.ErrInternalServerError
	}

	if len(notifiers) != len(ids) {
		return fiber.NewError(fiber.StatusBadRequest, "one or more notifier ids do not exist")
	}

	for i := range notifiers {
		if notifiers[i].Status != entity.NotifierStatusVerified {
			return fiber.NewError(fiber.StatusUnprocessableEntity,
				"notifier "+notifiers[i].Name+" is not verified yet")
		}
	}

	if err := c.Events.ReplaceNotifiers(tx, eventID, ids); err != nil {
		return fiber.ErrInternalServerError
	}

	return nil
}

func (c *WatcherEventUseCase) List(ctx context.Context, request *model.ListWatcherEventRequest) ([]model.WatcherEventResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		return nil, fiber.ErrBadRequest
	}

	db := c.DB.WithContext(ctx)

	if _, err := c.ownedWatcher(db, request.WatcherID, request.UserID); err != nil {
		return nil, err
	}

	events, err := c.Events.FindByWatcher(db, request.WatcherID)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	ids := make([]string, 0, len(events))
	for i := range events {
		ids = append(ids, events[i].ID)
	}

	notifiers, err := c.Events.FindNotifiersForEvents(db, ids)
	if err == nil {
		for i := range events {
			events[i].Notifiers = notifiers[events[i].ID]
		}
	}

	return converter.EventsToResponses(events), nil
}

func (c *WatcherEventUseCase) Get(ctx context.Context, request *model.GetWatcherEventRequest) (*model.WatcherEventResponse, error) {
	db := c.DB.WithContext(ctx)

	if _, err := c.ownedWatcher(db, request.WatcherID, request.UserID); err != nil {
		return nil, err
	}

	watcherEvent := new(entity.WatcherEvent)
	if err := c.Events.FindByIdAndWatcher(db, watcherEvent, request.ID, request.WatcherID); err != nil {
		return nil, fiber.NewError(fiber.StatusNotFound, "event not found")
	}

	return c.load(db, watcherEvent)
}

func (c *WatcherEventUseCase) load(db *gorm.DB, watcherEvent *entity.WatcherEvent) (*model.WatcherEventResponse, error) {
	notifiers, err := c.Events.FindNotifiers(db, watcherEvent.ID)
	if err == nil {
		watcherEvent.Notifiers = notifiers
	}
	return converter.EventToResponse(watcherEvent), nil
}

func (c *WatcherEventUseCase) Update(ctx context.Context, request *model.UpdateWatcherEventRequest) (*model.WatcherEventResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		return nil, fiber.ErrBadRequest
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	if _, err := c.ownedWatcher(tx, request.WatcherID, request.UserID); err != nil {
		return nil, err
	}

	watcherEvent := new(entity.WatcherEvent)
	if err := c.Events.FindByIdAndWatcher(tx, watcherEvent, request.ID, request.WatcherID); err != nil {
		return nil, fiber.NewError(fiber.StatusNotFound, "event not found")
	}

	handler, err := c.Handlers.Get(watcherEvent.Type)
	if err != nil {
		return nil, err
	}

	if len(request.Config) > 0 {
		if err := handler.Validate(request.Config); err != nil {
			return nil, err
		}
		watcherEvent.Config = entity.JSON(request.Config)
	}

	if request.Position != nil {
		watcherEvent.Position = *request.Position
	}
	if request.Enabled != nil {
		watcherEvent.Enabled = *request.Enabled
	}
	if request.RunMode != "" {
		watcherEvent.RunMode = request.RunMode
	}
	if request.DelaySeconds != nil {
		watcherEvent.DelaySeconds = *request.DelaySeconds
	}
	if request.RepeatIntervalSeconds != nil {
		watcherEvent.RepeatIntervalSeconds = request.RepeatIntervalSeconds
	}
	if request.RepeatMax != nil {
		watcherEvent.RepeatMax = request.RepeatMax
	}
	if request.RepeatUntil != nil {
		watcherEvent.RepeatUntil = request.RepeatUntil
	}
	if request.CronExpression != nil {
		watcherEvent.CronExpression = nilIfEmpty(*request.CronExpression)
	}
	if request.StopOnAck != nil {
		watcherEvent.StopOnAck = *request.StopOnAck
	}

	if watcherEvent.RunMode == entity.RunModeRecurring &&
		watcherEvent.RepeatIntervalSeconds == nil && watcherEvent.CronExpression == nil {
		return nil, fiber.NewError(fiber.StatusBadRequest,
			"a recurring event needs either repeat_interval_seconds or a cron_expression")
	}

	if err := c.Events.Update(tx, watcherEvent); err != nil {
		return nil, fiber.ErrInternalServerError
	}

	if request.NotifierIDs != nil && handler.UsesNotifiers() {
		if err := c.attachNotifiers(tx, watcherEvent.ID, request.UserID, request.NotifierIDs); err != nil {
			return nil, err
		}
	}

	c.Audit.Record(ctx, tx, AuditEntry{ActorID: &request.UserID, Action: "watcher_event.updated",
		EntityType: "watcher_events", EntityID: &watcherEvent.ID})

	if err := tx.Commit().Error; err != nil {
		return nil, fiber.ErrInternalServerError
	}

	return c.load(c.DB.WithContext(ctx), watcherEvent)
}

func (c *WatcherEventUseCase) Delete(ctx context.Context, request *model.GetWatcherEventRequest) (bool, error) {
	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	if _, err := c.ownedWatcher(tx, request.WatcherID, request.UserID); err != nil {
		return false, err
	}

	watcherEvent := new(entity.WatcherEvent)
	if err := c.Events.FindByIdAndWatcher(tx, watcherEvent, request.ID, request.WatcherID); err != nil {
		return false, fiber.NewError(fiber.StatusNotFound, "event not found")
	}

	// pending runs would otherwise fire against a deleted event
	if err := tx.Model(&entity.EventRun{}).
		Where("watcher_event_id = ? AND status = ?", watcherEvent.ID, entity.RunStatusPending).
		Update("status", entity.RunStatusCancelled).Error; err != nil {
		return false, fiber.ErrInternalServerError
	}

	if err := c.Events.Delete(tx, watcherEvent); err != nil {
		return false, fiber.ErrInternalServerError
	}

	c.Audit.Record(ctx, tx, AuditEntry{ActorID: &request.UserID, Action: "watcher_event.deleted",
		EntityType: "watcher_events", EntityID: &watcherEvent.ID})

	if err := tx.Commit().Error; err != nil {
		return false, fiber.ErrInternalServerError
	}

	return true, nil
}

func (c *WatcherEventUseCase) Reorder(ctx context.Context, request *model.ReorderWatcherEventRequest) ([]model.WatcherEventResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		return nil, fiber.ErrBadRequest
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	if _, err := c.ownedWatcher(tx, request.WatcherID, request.UserID); err != nil {
		return nil, err
	}

	existing, err := c.Events.FindByWatcher(tx, request.WatcherID)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	owned := map[string]bool{}
	for i := range existing {
		owned[existing[i].ID] = true
	}

	for position, id := range request.EventIDs {
		if !owned[id] {
			return nil, fiber.NewError(fiber.StatusBadRequest, "event "+id+" does not belong to this watcher")
		}
		if err := c.Events.UpdatePosition(tx, id, position); err != nil {
			return nil, fiber.ErrInternalServerError
		}
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fiber.ErrInternalServerError
	}

	return c.List(ctx, &model.ListWatcherEventRequest{UserID: request.UserID, WatcherID: request.WatcherID})
}

// Test fires one occurrence for real against a sample match.
func (c *WatcherEventUseCase) Test(ctx context.Context, request *model.TestWatcherEventRequest) (*model.EventRunResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		return nil, fiber.ErrBadRequest
	}

	db := c.DB.WithContext(ctx)

	watcher, err := c.ownedWatcher(db, request.WatcherID, request.UserID)
	if err != nil {
		return nil, err
	}

	watcherEvent := new(entity.WatcherEvent)
	if err := c.Events.FindByIdAndWatcher(db, watcherEvent, request.ID, request.WatcherID); err != nil {
		return nil, fiber.NewError(fiber.StatusNotFound, "event not found")
	}

	match := new(entity.MatchedEmail)
	if request.MatchedEmailID != "" {
		if err := c.Matches.FindByIdAndUser(db, match, request.MatchedEmailID, request.UserID); err != nil {
			return nil, fiber.NewError(fiber.StatusNotFound, "matched email not found")
		}
	} else {
		// no match to hand: use the most recent one for this watcher
		recent, err := c.Matches.FindRecent(db, request.UserID, 1)
		if err != nil || len(recent) == 0 {
			return nil, fiber.NewError(fiber.StatusUnprocessableEntity,
				"there is no matched email to test against yet: sync the mailbox first, or pass matched_email_id")
		}
		*match = recent[0]
	}

	run := &entity.EventRun{
		ID:             uuid.NewString(),
		UserID:         request.UserID,
		MatchedEmailID: match.ID,
		WatcherEventID: watcherEvent.ID,
		// a test never collides with the real chain, which starts at 1
		Occurrence:     testOccurrence(db, c.Runs, match.ID, watcherEvent.ID),
		Status:         entity.RunStatusRunning,
		ScheduledAt:    time.Now().UnixMilli(),
		StartedAt:      ptr(time.Now().UnixMilli()),
		MaxAttempts:    1,
		ConfigSnapshot: entity.JSONOrEmpty(watcherEvent.Config, "{}"),
	}

	if err := c.Runs.Create(db, run); err != nil {
		c.Log.Warnf("Failed create test run : %+v", err)
		return nil, fiber.ErrInternalServerError
	}

	c.Dispatcher.Execute(ctx, run)

	reloaded := new(entity.EventRun)
	if err := c.Runs.FindById(db, reloaded, run.ID); err != nil {
		return nil, fiber.ErrInternalServerError
	}

	deliveries, err := c.Dispatcher.Deliveries.FindByRun(db, reloaded.ID)
	if err == nil {
		reloaded.Deliveries = deliveries
	}

	response := converter.EventRunToResponse(reloaded)
	response.EventType = watcherEvent.Type
	_ = watcher

	return response, nil
}

// testOccurrence picks a number above every existing occurrence so the unique
// (match, event, occurrence) index is not violated by repeated tests.
func testOccurrence(db *gorm.DB, runs *repository.EventRunRepository, matchID, eventID string) int {
	var highest *int
	err := db.Model(&entity.EventRun{}).
		Where("matched_email_id = ? AND watcher_event_id = ?", matchID, eventID).
		Select("MAX(occurrence)").
		Scan(&highest).Error
	if err != nil || highest == nil {
		return 1000
	}
	if *highest < 1000 {
		return 1000
	}
	return *highest + 1
}
