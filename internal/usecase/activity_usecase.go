package usecase

import (
	"context"
	"time"

	"mailpulse/internal/entity"
	"mailpulse/internal/model"
	"mailpulse/internal/model/converter"
	"mailpulse/internal/repository"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// ActivityUseCase serves the read-only trail: matches, runs, deliveries and
// the dashboard rollup, plus the three actions a user can take on a run.
type ActivityUseCase struct {
	DB         *gorm.DB
	Log        *logrus.Logger
	Validate   *validator.Validate
	Matches    *repository.MatchedEmailRepository
	Runs       *repository.EventRunRepository
	Deliveries *repository.NotificationDeliveryRepository
	Events     *repository.WatcherEventRepository
	Watchers   *repository.WatcherRepository
	Accounts   *repository.MailAccountRepository
	Notifiers  *repository.NotifierRepository
	Dispatcher *DispatcherUseCase
	Audit      *AuditUseCase
}

func NewActivityUseCase(db *gorm.DB, log *logrus.Logger, validate *validator.Validate,
	matches *repository.MatchedEmailRepository, runs *repository.EventRunRepository,
	deliveries *repository.NotificationDeliveryRepository, events *repository.WatcherEventRepository,
	watchers *repository.WatcherRepository, accounts *repository.MailAccountRepository,
	notifiers *repository.NotifierRepository, audit *AuditUseCase) *ActivityUseCase {
	return &ActivityUseCase{
		DB: db, Log: log, Validate: validate,
		Matches: matches, Runs: runs, Deliveries: deliveries, Events: events,
		Watchers: watchers, Accounts: accounts, Notifiers: notifiers, Audit: audit,
	}
}

func (c *ActivityUseCase) SetDispatcher(dispatcher *DispatcherUseCase) {
	c.Dispatcher = dispatcher
}

func (c *ActivityUseCase) ListMatches(ctx context.Context, request *model.ListMatchedEmailRequest) ([]model.MatchedEmailResponse, *model.PageMetadata, error) {
	request.Normalize()
	if err := c.Validate.Struct(request); err != nil {
		return nil, nil, fiber.ErrBadRequest
	}

	query := c.Matches.Search(c.DB.WithContext(ctx), repository.MatchSearch{
		UserID:        request.UserID,
		WatcherID:     request.WatcherID,
		MailAccountID: request.MailAccountID,
		From:          request.From,
		To:            request.To,
		Query:         request.Query,
	})

	var matches []entity.MatchedEmail
	total, err := c.Matches.Paginate(query, &matches, request.Page, request.Size)
	if err != nil {
		c.Log.Warnf("Failed list matches : %+v", err)
		return nil, nil, fiber.ErrInternalServerError
	}

	metadata := model.NewPageMetadata(request.Page, request.Size, total)
	return converter.MatchedEmailsToResponses(matches), &metadata, nil
}

// GetMatch answers "why did this fire, and what happened after?" in one read.
func (c *ActivityUseCase) GetMatch(ctx context.Context, request *model.GetMatchedEmailRequest) (*model.MatchedEmailResponse, error) {
	db := c.DB.WithContext(ctx)

	match := new(entity.MatchedEmail)
	if request.UserID != "" {
		if err := c.Matches.FindByIdAndUser(db, match, request.ID, request.UserID); err != nil {
			return nil, fiber.NewError(fiber.StatusNotFound, "match not found")
		}
	} else if err := c.Matches.FindById(db, match, request.ID); err != nil {
		return nil, fiber.NewError(fiber.StatusNotFound, "match not found")
	}

	runs, err := c.Runs.FindByMatch(db, match.ID)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	runIDs := make([]string, 0, len(runs))
	for i := range runs {
		runIDs = append(runIDs, runs[i].ID)
	}

	grouped, err := c.Deliveries.FindByRuns(db, runIDs)
	if err == nil {
		for i := range runs {
			runs[i].Deliveries = grouped[runs[i].ID]
		}
	}
	match.Runs = runs

	response := converter.MatchedEmailToResponse(match)

	watcher := new(entity.Watcher)
	if err := c.Watchers.FindById(db, watcher, match.WatcherID); err == nil {
		response.WatcherName = watcher.Name
	}

	return response, nil
}

func (c *ActivityUseCase) ListRuns(ctx context.Context, request *model.ListEventRunRequest) ([]model.EventRunResponse, *model.PageMetadata, error) {
	request.Normalize()
	if err := c.Validate.Struct(request); err != nil {
		return nil, nil, fiber.ErrBadRequest
	}

	query := c.Runs.Search(c.DB.WithContext(ctx), repository.RunSearch{
		UserID:    request.UserID,
		WatcherID: request.WatcherID,
		Status:    request.Status,
		From:      request.From,
		To:        request.To,
	})

	var runs []entity.EventRun
	total, err := c.Runs.Paginate(query, &runs, request.Page, request.Size)
	if err != nil {
		c.Log.Warnf("Failed list runs : %+v", err)
		return nil, nil, fiber.ErrInternalServerError
	}

	metadata := model.NewPageMetadata(request.Page, request.Size, total)
	return converter.EventRunsToResponses(runs), &metadata, nil
}

func (c *ActivityUseCase) GetRun(ctx context.Context, request *model.GetEventRunRequest) (*model.EventRunResponse, error) {
	db := c.DB.WithContext(ctx)

	run, err := c.findRun(db, request.ID, request.UserID)
	if err != nil {
		return nil, err
	}

	deliveries, err := c.Deliveries.FindByRun(db, run.ID)
	if err == nil {
		run.Deliveries = deliveries
	}

	response := converter.EventRunToResponse(run)

	watcherEvent := new(entity.WatcherEvent)
	if err := c.Events.FindById(db, watcherEvent, run.WatcherEventID); err == nil {
		response.EventType = watcherEvent.Type
	}

	return response, nil
}

func (c *ActivityUseCase) findRun(db *gorm.DB, id, userID string) (*entity.EventRun, error) {
	run := new(entity.EventRun)
	if userID != "" {
		if err := c.Runs.FindByIdAndUser(db, run, id, userID); err != nil {
			return nil, fiber.NewError(fiber.StatusNotFound, "run not found")
		}
		return run, nil
	}

	if err := c.Runs.FindById(db, run, id); err != nil {
		return nil, fiber.NewError(fiber.StatusNotFound, "run not found")
	}
	return run, nil
}

// Retry re-runs a failed occurrence immediately.
func (c *ActivityUseCase) Retry(ctx context.Context, request *model.EventRunActionRequest) (*model.EventRunResponse, error) {
	db := c.DB.WithContext(ctx)

	run, err := c.findRun(db, request.ID, request.UserID)
	if err != nil {
		return nil, err
	}

	if run.Status == entity.RunStatusRunning {
		return nil, fiber.NewError(fiber.StatusConflict, "that run is already in flight")
	}

	run.Status = entity.RunStatusRunning
	run.Attempt = 0
	run.Error = nil
	run.FinishedAt = nil
	run.StartedAt = ptr(time.Now().UnixMilli())

	if err := c.Runs.Update(db, run); err != nil {
		return nil, fiber.ErrInternalServerError
	}

	c.Dispatcher.Execute(ctx, run)

	return c.GetRun(ctx, &model.GetEventRunRequest{UserID: request.UserID, ID: request.ID})
}

// Cancel stops this occurrence and every one still scheduled after it.
func (c *ActivityUseCase) Cancel(ctx context.Context, request *model.EventRunActionRequest) (*model.CancelRunResponse, error) {
	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	run, err := c.findRun(tx, request.ID, request.UserID)
	if err != nil {
		return nil, err
	}

	cancelled, err := c.Runs.CancelPendingAfter(tx, run.MatchedEmailID, run.WatcherEventID, run.Occurrence)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	c.Audit.Record(tx, AuditEntry{ActorID: &request.UserID, Action: "event_run.cancelled",
		EntityType: "event_runs", EntityID: &run.ID})

	if err := tx.Commit().Error; err != nil {
		return nil, fiber.ErrInternalServerError
	}

	return &model.CancelRunResponse{Cancelled: cancelled}, nil
}

// Ack marks a run acknowledged, and ends the repeat chain when the event was
// configured to stop on acknowledgement.
func (c *ActivityUseCase) Ack(ctx context.Context, request *model.EventRunActionRequest) (*model.AckRunResponse, error) {
	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	run, err := c.findRun(tx, request.ID, request.UserID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	run.AcknowledgedAt = &now
	if err := c.Runs.Update(tx, run); err != nil {
		return nil, fiber.ErrInternalServerError
	}

	var cancelled int64
	watcherEvent := new(entity.WatcherEvent)
	if err := c.Events.FindById(tx, watcherEvent, run.WatcherEventID); err == nil && watcherEvent.StopOnAck {
		cancelled, err = c.Runs.CancelPendingAfter(tx, run.MatchedEmailID, run.WatcherEventID, run.Occurrence+1)
		if err != nil {
			return nil, fiber.ErrInternalServerError
		}
	}

	c.Audit.Record(tx, AuditEntry{ActorID: &request.UserID, Action: "event_run.acknowledged",
		EntityType: "event_runs", EntityID: &run.ID})

	if err := tx.Commit().Error; err != nil {
		return nil, fiber.ErrInternalServerError
	}

	return &model.AckRunResponse{AcknowledgedAt: now, CancelledOccurrences: cancelled}, nil
}

func (c *ActivityUseCase) ListDeliveries(ctx context.Context, request *model.ListDeliveryRequest) ([]model.DeliveryResponse, *model.PageMetadata, error) {
	request.Normalize()
	if err := c.Validate.Struct(request); err != nil {
		return nil, nil, fiber.ErrBadRequest
	}

	query := c.Deliveries.Search(c.DB.WithContext(ctx), request.UserID, request.EventRunID, request.NotifierID, request.Status)

	var deliveries []entity.NotificationDelivery
	total, err := c.Deliveries.Paginate(query, &deliveries, request.Page, request.Size)
	if err != nil {
		c.Log.Warnf("Failed list deliveries : %+v", err)
		return nil, nil, fiber.ErrInternalServerError
	}

	metadata := model.NewPageMetadata(request.Page, request.Size, total)
	return converter.DeliveriesToResponses(deliveries), &metadata, nil
}

// Dashboard is one request for the landing screen rather than six.
func (c *ActivityUseCase) Dashboard(ctx context.Context, request *model.DashboardSummaryRequest) (*model.DashboardSummaryResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		return nil, fiber.ErrBadRequest
	}

	db := c.DB.WithContext(ctx)
	since := time.Now().Add(-24 * time.Hour).UnixMilli()

	watcherCounts, err := c.Watchers.CountByStatusForUser(db, request.UserID)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	accountCounts, err := c.Accounts.CountByStatus(db.Where("user_id = ?", request.UserID))
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	notifierCounts, err := c.Notifiers.CountByStatus(db.Where("user_id = ?", request.UserID))
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	matches24h, err := c.Matches.CountSince(db, request.UserID, since)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	failed24h, err := c.Runs.CountFailedSince(db, request.UserID, since)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	response := &model.DashboardSummaryResponse{
		Watchers:      model.StatusCounts(watcherCounts),
		MailAccounts:  model.StatusCounts(accountCounts),
		Notifiers:     model.StatusCounts(notifierCounts),
		Matches24h:    matches24h,
		RunsFailed24h: failed24h,
		Recent:        []model.RecentActivity{},
	}

	recent, err := c.Matches.FindRecent(db, request.UserID, 10)
	if err == nil {
		names := c.watcherNames(db, recent)
		for i := range recent {
			response.Recent = append(response.Recent, model.RecentActivity{
				Type:        "match",
				ID:          recent[i].ID,
				WatcherName: names[recent[i].WatcherID],
				Subject:     derefString(recent[i].Subject),
				At:          recent[i].MatchedAt,
			})
		}
	}

	return response, nil
}

func (c *ActivityUseCase) watcherNames(db *gorm.DB, matches []entity.MatchedEmail) map[string]string {
	names := map[string]string{}
	if len(matches) == 0 {
		return names
	}

	ids := make([]string, 0, len(matches))
	for i := range matches {
		ids = append(ids, matches[i].WatcherID)
	}

	type row struct {
		ID   string
		Name string
	}
	var rows []row
	if err := db.Model(&entity.Watcher{}).Select("id, name").Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		return names
	}

	for _, item := range rows {
		names[item.ID] = item.Name
	}
	return names
}
