package usecase

import (
	"context"
	"time"

	"mailpulse/internal/entity"
	"mailpulse/internal/gateway/mail"
	"mailpulse/internal/model"
	"mailpulse/internal/model/converter"
	"mailpulse/internal/repository"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type WatcherUseCase struct {
	DB       *gorm.DB
	Log      *logrus.Logger
	Validate *validator.Validate
	Watchers *repository.WatcherRepository
	Filters  *repository.WatcherFilterRepository
	Events   *repository.WatcherEventRepository
	Accounts *repository.MailAccountRepository
	Matches  *repository.MatchedEmailRepository
	Runs     *repository.EventRunRepository
	EventUC  *WatcherEventUseCase
	Audit    *AuditUseCase
}

func NewWatcherUseCase(db *gorm.DB, log *logrus.Logger, validate *validator.Validate,
	watchers *repository.WatcherRepository, filters *repository.WatcherFilterRepository,
	events *repository.WatcherEventRepository, accounts *repository.MailAccountRepository,
	matches *repository.MatchedEmailRepository, runs *repository.EventRunRepository,
	eventUC *WatcherEventUseCase, audit *AuditUseCase) *WatcherUseCase {
	return &WatcherUseCase{
		DB: db, Log: log, Validate: validate,
		Watchers: watchers, Filters: filters, Events: events, Accounts: accounts,
		Matches: matches, Runs: runs, EventUC: eventUC, Audit: audit,
	}
}

// Create accepts filters and events inline so building a watcher is one
// request rather than three round trips from the wizard.
func (c *WatcherUseCase) Create(ctx context.Context, request *model.CreateWatcherRequest) (*model.WatcherResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		c.Log.Warnf("Invalid watcher request : %+v", err)
		return nil, fiber.ErrBadRequest
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	account := new(entity.MailAccount)
	if err := c.Accounts.FindByIdAndUser(tx, account, request.MailAccountID, request.UserID); err != nil {
		return nil, fiber.NewError(fiber.StatusBadRequest, "that mail account does not exist")
	}

	matchMode := request.MatchMode
	if matchMode == "" {
		matchMode = entity.MatchModeAll
	}
	folder := request.Folder
	if folder == "" {
		folder = "INBOX"
	}

	watchFrom := request.WatchFrom
	if watchFrom == nil {
		// default to "from now on": a new watcher that immediately fires on a
		// year of history is a worse surprise than one that starts quiet
		watchFrom = ptr(time.Now().UnixMilli())
	}

	watcher := &entity.Watcher{
		ID:              uuid.NewString(),
		UserID:          request.UserID,
		MailAccountID:   account.ID,
		Name:            request.Name,
		Description:     nilIfEmpty(request.Description),
		Status:          entity.WatcherStatusActive,
		MatchMode:       matchMode,
		Folder:          folder,
		WatchFrom:       watchFrom,
		CooldownSeconds: request.CooldownSeconds,
	}

	if err := c.Watchers.Create(tx, watcher); err != nil {
		c.Log.Warnf("Failed create watcher : %+v", err)
		return nil, fiber.ErrInternalServerError
	}

	if err := c.writeFilters(tx, watcher.ID, request.Filters); err != nil {
		return nil, err
	}

	for i := range request.Events {
		event := request.Events[i]
		event.UserID = request.UserID
		event.WatcherID = watcher.ID
		if _, err := c.EventUC.createWithin(tx, &event); err != nil {
			return nil, err
		}
	}

	c.Audit.Record(tx, AuditEntry{ActorID: &request.UserID, Action: "watcher.created",
		EntityType: "watchers", EntityID: &watcher.ID})

	if err := tx.Commit().Error; err != nil {
		return nil, fiber.ErrInternalServerError
	}

	return c.Get(ctx, &model.GetWatcherRequest{UserID: request.UserID, ID: watcher.ID})
}

func (c *WatcherUseCase) writeFilters(tx *gorm.DB, watcherID string, requests []model.WatcherFilterRequest) error {
	if len(requests) == 0 {
		return nil
	}

	filters := make([]entity.WatcherFilter, 0, len(requests))
	for i, item := range requests {
		if item.Field == entity.FieldHeader && item.HeaderName == "" {
			return fiber.NewError(fiber.StatusBadRequest, "a header filter needs a header_name")
		}

		position := item.Position
		if position == 0 {
			position = i
		}

		filters = append(filters, entity.WatcherFilter{
			ID:            uuid.NewString(),
			WatcherID:     watcherID,
			Field:         item.Field,
			HeaderName:    nilIfEmpty(item.HeaderName),
			Operator:      item.Operator,
			Value:         item.Value,
			CaseSensitive: item.CaseSensitive,
			Position:      position,
		})
	}

	if err := c.Filters.CreateInBatch(tx, filters, 50); err != nil {
		c.Log.Warnf("Failed create filters : %+v", err)
		return fiber.ErrInternalServerError
	}

	return nil
}

func (c *WatcherUseCase) List(ctx context.Context, request *model.ListWatcherRequest) ([]model.WatcherResponse, *model.PageMetadata, error) {
	request.Normalize()
	if err := c.Validate.Struct(request); err != nil {
		return nil, nil, fiber.ErrBadRequest
	}

	db := c.DB.WithContext(ctx)
	query := c.Watchers.Search(db, request.UserID, request.Status, request.MailAccountID, request.Query)

	var watchers []entity.Watcher
	total, err := c.Watchers.Paginate(query, &watchers, request.Page, request.Size)
	if err != nil {
		c.Log.Warnf("Failed list watchers : %+v", err)
		return nil, nil, fiber.ErrInternalServerError
	}

	// one query for every watcher's filters rather than one per row
	ids := make([]string, 0, len(watchers))
	for i := range watchers {
		ids = append(ids, watchers[i].ID)
	}

	grouped, err := c.Filters.FindByWatchers(db, ids)
	if err == nil {
		for i := range watchers {
			watchers[i].Filters = grouped[watchers[i].ID]
		}
	}

	metadata := model.NewPageMetadata(request.Page, request.Size, total)
	return converter.WatchersToResponses(watchers), &metadata, nil
}

func (c *WatcherUseCase) Get(ctx context.Context, request *model.GetWatcherRequest) (*model.WatcherResponse, error) {
	db := c.DB.WithContext(ctx)

	watcher, err := c.find(db, request.ID, request.UserID)
	if err != nil {
		return nil, err
	}

	filters, err := c.Filters.FindByWatcher(db, watcher.ID)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}
	watcher.Filters = filters

	events, err := c.Events.FindByWatcher(db, watcher.ID)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	eventIDs := make([]string, 0, len(events))
	for i := range events {
		eventIDs = append(eventIDs, events[i].ID)
	}
	notifiers, err := c.Events.FindNotifiersForEvents(db, eventIDs)
	if err == nil {
		for i := range events {
			events[i].Notifiers = notifiers[events[i].ID]
		}
	}
	watcher.Events = events

	account := new(entity.MailAccount)
	if err := c.Accounts.FindById(db, account, watcher.MailAccountID); err == nil {
		watcher.MailAccount = account
	}

	return converter.WatcherToResponse(watcher), nil
}

func (c *WatcherUseCase) find(db *gorm.DB, id, userID string) (*entity.Watcher, error) {
	watcher := new(entity.Watcher)
	if err := c.Watchers.FindByIdAndUser(db, watcher, id, userID); err != nil {
		return nil, fiber.NewError(fiber.StatusNotFound, "watcher not found")
	}
	return watcher, nil
}

func (c *WatcherUseCase) Update(ctx context.Context, request *model.UpdateWatcherRequest) (*model.WatcherResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		return nil, fiber.ErrBadRequest
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	watcher, err := c.find(tx, request.ID, request.UserID)
	if err != nil {
		return nil, err
	}

	if request.Name != "" {
		watcher.Name = request.Name
	}
	if request.Description != nil {
		watcher.Description = nilIfEmpty(*request.Description)
	}
	if request.MatchMode != "" {
		watcher.MatchMode = request.MatchMode
	}
	if request.Folder != "" {
		watcher.Folder = request.Folder
	}
	if request.CooldownSeconds != nil {
		watcher.CooldownSeconds = *request.CooldownSeconds
	}

	if err := c.Watchers.Update(tx, watcher); err != nil {
		return nil, fiber.ErrInternalServerError
	}

	c.Audit.Record(tx, AuditEntry{ActorID: &request.UserID, Action: "watcher.updated",
		EntityType: "watchers", EntityID: &watcher.ID})

	if err := tx.Commit().Error; err != nil {
		return nil, fiber.ErrInternalServerError
	}

	return c.Get(ctx, &model.GetWatcherRequest{UserID: request.UserID, ID: watcher.ID})
}

func (c *WatcherUseCase) Delete(ctx context.Context, request *model.GetWatcherRequest) (bool, error) {
	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	watcher, err := c.find(tx, request.ID, request.UserID)
	if err != nil {
		return false, err
	}

	if err := c.Watchers.Delete(tx, watcher); err != nil {
		c.Log.Warnf("Failed delete watcher : %+v", err)
		return false, fiber.ErrInternalServerError
	}

	c.Audit.Record(tx, AuditEntry{ActorID: &request.UserID, Action: "watcher.deleted",
		EntityType: "watchers", EntityID: &watcher.ID})

	if err := tx.Commit().Error; err != nil {
		return false, fiber.ErrInternalServerError
	}

	return true, nil
}

// SetStatus backs _archive, _restore, _pause and _resume.
func (c *WatcherUseCase) SetStatus(ctx context.Context, request *model.SetWatcherStatusRequest) (*model.WatcherResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		return nil, fiber.ErrBadRequest
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	watcher, err := c.find(tx, request.ID, request.UserID)
	if err != nil {
		return nil, err
	}

	watcher.Status = request.Status
	if request.Status == entity.WatcherStatusArchived {
		watcher.ArchivedAt = ptr(time.Now().UnixMilli())
	} else {
		watcher.ArchivedAt = nil
	}

	if err := c.Watchers.Update(tx, watcher); err != nil {
		return nil, fiber.ErrInternalServerError
	}

	c.Audit.Record(tx, AuditEntry{ActorID: &request.UserID, Action: "watcher." + request.Status,
		EntityType: "watchers", EntityID: &watcher.ID})

	if err := tx.Commit().Error; err != nil {
		return nil, fiber.ErrInternalServerError
	}

	return c.Get(ctx, &model.GetWatcherRequest{UserID: request.UserID, ID: watcher.ID})
}

func (c *WatcherUseCase) GetFilters(ctx context.Context, request *model.GetWatcherRequest) ([]model.WatcherFilterResponse, error) {
	db := c.DB.WithContext(ctx)

	watcher, err := c.find(db, request.ID, request.UserID)
	if err != nil {
		return nil, err
	}

	filters, err := c.Filters.FindByWatcher(db, watcher.ID)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	return converter.FiltersToResponses(filters), nil
}

// ReplaceFilters swaps the whole set, because the UI edits them as one form.
func (c *WatcherUseCase) ReplaceFilters(ctx context.Context, request *model.ReplaceFiltersRequest) ([]model.WatcherFilterResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		return nil, fiber.ErrBadRequest
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	watcher, err := c.find(tx, request.WatcherID, request.UserID)
	if err != nil {
		return nil, err
	}

	if request.MatchMode != "" && request.MatchMode != watcher.MatchMode {
		watcher.MatchMode = request.MatchMode
		if err := c.Watchers.Update(tx, watcher); err != nil {
			return nil, fiber.ErrInternalServerError
		}
	}

	if err := c.Filters.DeleteByWatcher(tx, watcher.ID); err != nil {
		return nil, fiber.ErrInternalServerError
	}

	if err := c.writeFilters(tx, watcher.ID, request.Filters); err != nil {
		return nil, err
	}

	c.Audit.Record(tx, AuditEntry{ActorID: &request.UserID, Action: "watcher.filters_replaced",
		EntityType: "watchers", EntityID: &watcher.ID})

	if err := tx.Commit().Error; err != nil {
		return nil, fiber.ErrInternalServerError
	}

	filters, err := c.Filters.FindByWatcher(c.DB.WithContext(ctx), watcher.ID)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	return converter.FiltersToResponses(filters), nil
}

// Test dry-runs the filters against recent mail and sends nothing. It turns
// writing filters from guesswork into feedback.
func (c *WatcherUseCase) Test(ctx context.Context, request *model.TestWatcherRequest, providers *mail.Registry,
	accountUC *MailAccountUseCase) (*model.TestWatcherResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		return nil, fiber.ErrBadRequest
	}

	db := c.DB.WithContext(ctx)

	watcher, err := c.find(db, request.ID, request.UserID)
	if err != nil {
		return nil, err
	}

	filters, err := c.Filters.FindByWatcher(db, watcher.ID)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	account := new(entity.MailAccount)
	if err := c.Accounts.FindById(db, account, watcher.MailAccountID); err != nil {
		return nil, fiber.ErrNotFound
	}

	provider, err := providers.Get(account.Provider)
	if err != nil {
		return nil, err
	}

	target, err := accountUC.providerAccount(account)
	if err != nil {
		return nil, err
	}

	size := request.SampleSize
	if size == 0 {
		size = 50
	}

	// no cursor: a dry run always looks at the most recent mail, and must not
	// advance the real sync position
	result, err := provider.Fetch(ctx, target, mail.FetchRequest{Folder: watcher.Folder, Limit: size})
	if err != nil {
		return nil, fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	}

	response := &model.TestWatcherResponse{
		Scanned: len(result.Messages),
		Samples: make([]model.TestWatcherSample, 0, len(result.Messages)),
	}

	for i := range result.Messages {
		message := result.Messages[i]
		outcome := EvaluateFilters(watcher, filters, &message)
		if outcome.Matched {
			response.Matched++
		}

		response.Samples = append(response.Samples, model.TestWatcherSample{
			Subject:        message.Subject,
			FromAddress:    message.FromAddress,
			ReceivedAt:     message.ReceivedAt,
			Matched:        outcome.Matched,
			MatchedFilters: outcome.Descriptions,
		})
	}

	return response, nil
}

func (c *WatcherUseCase) Stats(ctx context.Context, request *model.WatcherStatsRequest) (*model.WatcherStatsResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		return nil, fiber.ErrBadRequest
	}

	db := c.DB.WithContext(ctx)

	watcher, err := c.find(db, request.ID, request.UserID)
	if err != nil {
		return nil, err
	}

	days := request.Days
	if days == 0 {
		days = 30
	}
	since := time.Now().AddDate(0, 0, -days).UnixMilli()

	total, err := c.Matches.CountForWatcher(db, watcher.ID)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	runCounts, err := c.Runs.CountByStatusForWatcher(db, watcher.ID)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	daily, err := c.Matches.DailyCounts(db, watcher.ID, since)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	response := &model.WatcherStatsResponse{
		TotalMatches:  total,
		RunsSucceeded: runCounts[entity.RunStatusSucceeded],
		RunsFailed:    runCounts[entity.RunStatusFailed],
		RunsPending:   runCounts[entity.RunStatusPending],
		Daily:         make([]model.DailyMatchCount, 0, len(daily)),
	}

	for _, item := range daily {
		response.Daily = append(response.Daily, model.DailyMatchCount{Date: item.Date, Matches: item.Total})
	}

	return response, nil
}
