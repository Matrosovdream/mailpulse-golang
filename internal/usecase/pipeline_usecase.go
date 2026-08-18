package usecase

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"mailpulse/internal/entity"
	"mailpulse/internal/gateway/mail"
	"mailpulse/internal/gateway/secret"
	"mailpulse/internal/model"
	"mailpulse/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// PipelineUseCase turns new mail into scheduled work: fetch a mailbox once,
// test every active watcher on it, record the matches, and expand each
// watcher's events into event_runs.
//
// Polling is per account rather than per watcher on purpose — ten watchers on
// one mailbox is one fetch, not ten.
type PipelineUseCase struct {
	DB       *gorm.DB
	Log      *logrus.Logger
	Accounts *repository.MailAccountRepository
	Watchers *repository.WatcherRepository
	Filters  *repository.WatcherFilterRepository
	Events   *repository.WatcherEventRepository
	Matches  *repository.MatchedEmailRepository
	Runs     *repository.EventRunRepository
	SyncRuns *repository.MailSyncRunRepository
	Provider *mail.Registry
	Cipher   *secret.Cipher
	Resolver *MailResolver
}

func NewPipelineUseCase(db *gorm.DB, log *logrus.Logger,
	accounts *repository.MailAccountRepository, watchers *repository.WatcherRepository,
	filters *repository.WatcherFilterRepository, events *repository.WatcherEventRepository,
	matches *repository.MatchedEmailRepository, runs *repository.EventRunRepository,
	syncRuns *repository.MailSyncRunRepository, provider *mail.Registry,
	cipher *secret.Cipher, resolver *MailResolver) *PipelineUseCase {
	return &PipelineUseCase{
		DB: db, Log: log, Accounts: accounts, Watchers: watchers, Filters: filters,
		Events: events, Matches: matches, Runs: runs, SyncRuns: syncRuns,
		Provider: provider, Cipher: cipher, Resolver: resolver,
	}
}

// PollDue is the worker's tick: claim the accounts whose poll came around and
// sync each one.
func (c *PipelineUseCase) PollDue(ctx context.Context, limit int) (int, error) {
	db := c.DB.WithContext(ctx)

	var accounts []entity.MailAccount
	err := db.Transaction(func(tx *gorm.DB) error {
		found, err := c.Accounts.FindDue(tx, time.Now().UnixMilli(), limit)
		if err != nil {
			return err
		}
		accounts = found

		// push next_poll_at forward inside the claim so a second worker
		// arriving now does not pick the same mailbox up again
		for i := range accounts {
			next := time.Now().Add(time.Duration(accounts[i].PollIntervalSeconds) * time.Second).UnixMilli()
			if err := tx.Model(&entity.MailAccount{}).Where("id = ?", accounts[i].ID).
				Update("next_poll_at", next).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	for i := range accounts {
		if _, err := c.SyncAccount(ctx, &accounts[i]); err != nil {
			c.Log.WithError(err).Warnf("Sync failed for mail account %s", accounts[i].ID)
		}
	}

	return len(accounts), nil
}

// SyncAccount fetches one mailbox and runs everything downstream of it.
func (c *PipelineUseCase) SyncAccount(ctx context.Context, account *entity.MailAccount) (*model.SyncMailAccountResponse, error) {
	db := c.DB.WithContext(ctx)

	syncRun := &entity.MailSyncRun{
		ID:            uuid.NewString(),
		MailAccountID: account.ID,
		Status:        entity.SyncStatusRunning,
		StartedAt:     time.Now().UnixMilli(),
	}
	if err := c.SyncRuns.Create(db, syncRun); err != nil {
		c.Log.WithError(err).Warn("Failed to open a sync run")
		return nil, fiber.ErrInternalServerError
	}

	finish := func(status string, fetched, matched int, cause error) {
		now := time.Now().UnixMilli()
		syncRun.Status = status
		syncRun.FinishedAt = &now
		syncRun.MessagesFetched = fetched
		syncRun.MatchesCreated = matched
		if cause != nil {
			syncRun.Error = ptr(cause.Error())
		}
		if err := c.SyncRuns.Update(db, syncRun); err != nil {
			c.Log.WithError(err).Warn("Failed to close the sync run")
		}
	}

	provider, target, err := c.Resolver.Resolve(db, account)
	if err != nil {
		finish(entity.SyncStatusError, 0, 0, err)
		return nil, err
	}

	watchers, err := c.Watchers.FindActiveByMailAccount(db, account.ID)
	if err != nil {
		finish(entity.SyncStatusError, 0, 0, err)
		return nil, fiber.ErrInternalServerError
	}

	folder := "INBOX"
	if len(watchers) > 0 && watchers[0].Folder != "" {
		folder = watchers[0].Folder
	}

	result, err := provider.Fetch(ctx, target, mail.FetchRequest{
		Folder: folder,
		Cursor: []byte(account.SyncState),
		Limit:  200,
	})
	if err != nil {
		finish(entity.SyncStatusError, 0, 0, err)
		account.Status = entity.MailAccountStatusError
		account.LastError = ptr(err.Error())
		_ = c.Accounts.Update(db, account)
		return nil, fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	}

	matchesCreated := 0
	for i := range result.Messages {
		created, err := c.evaluate(ctx, account, watchers, &result.Messages[i])
		if err != nil {
			c.Log.WithError(err).Warn("Failed to evaluate a message")
			continue
		}
		matchesCreated += created
	}

	now := time.Now().UnixMilli()
	account.LastSyncedAt = &now
	account.LastError = nil
	if len(result.Cursor) > 0 {
		account.SyncState = entity.JSON(result.Cursor)
	}
	if err := c.Accounts.Update(db, account); err != nil {
		c.Log.WithError(err).Warn("Failed to store the sync cursor")
	}

	finish(entity.SyncStatusOK, len(result.Messages), matchesCreated, nil)

	return &model.SyncMailAccountResponse{
		SyncRunID:       syncRun.ID,
		MessagesFetched: len(result.Messages),
		MatchesCreated:  matchesCreated,
		Status:          entity.SyncStatusOK,
	}, nil
}

// evaluate tests one message against every active watcher on the account.
func (c *PipelineUseCase) evaluate(ctx context.Context, account *entity.MailAccount,
	watchers []entity.Watcher, message *mail.Message) (int, error) {
	created := 0

	for i := range watchers {
		watcher := watchers[i]

		if watcher.WatchFrom != nil && message.ReceivedAt < *watcher.WatchFrom {
			continue
		}

		// cooldown throttles a noisy watcher without pausing it
		if watcher.CooldownSeconds > 0 && watcher.LastMatchedAt != nil {
			elapsed := time.Now().UnixMilli() - *watcher.LastMatchedAt
			if elapsed < int64(watcher.CooldownSeconds)*1000 {
				continue
			}
		}

		filters, err := c.Filters.FindByWatcher(c.DB.WithContext(ctx), watcher.ID)
		if err != nil {
			return created, err
		}

		outcome := EvaluateFilters(&watcher, filters, message)
		if !outcome.Matched {
			continue
		}

		match, isNew, err := c.recordMatch(ctx, account, &watcher, message, outcome)
		if err != nil {
			return created, err
		}
		if !isNew {
			// the unique (watcher_id, message_id) index already had this one
			continue
		}

		created++
		if err := c.ScheduleEvents(ctx, &watcher, match); err != nil {
			c.Log.WithError(err).Warnf("Failed to schedule events for match %s", match.ID)
		}
	}

	return created, nil
}

func (c *PipelineUseCase) recordMatch(ctx context.Context, account *entity.MailAccount,
	watcher *entity.Watcher, message *mail.Message, outcome MatchResult) (*entity.MatchedEmail, bool, error) {
	db := c.DB.WithContext(ctx)

	reasons, _ := json.Marshal(outcome.Descriptions)

	match := &entity.MatchedEmail{
		ID:             uuid.NewString(),
		UserID:         watcher.UserID,
		WatcherID:      watcher.ID,
		MailAccountID:  account.ID,
		MessageID:      message.MessageID,
		ProviderUID:    message.UID,
		Subject:        nilIfEmpty(message.Subject),
		FromAddress:    nilIfEmpty(message.FromAddress),
		FromName:       nilIfEmpty(message.FromName),
		ToAddresses:    nilIfEmpty(strings.Join(message.To, ", ")),
		Snippet:        nilIfEmpty(truncate(message.BodyText, 500)),
		HasAttachment:  message.HasAttachment,
		SizeBytes:      message.SizeBytes,
		ReceivedAt:     message.ReceivedAt,
		MatchedAt:      time.Now().UnixMilli(),
		MatchedFilters: entity.JSON(reasons),
	}

	isNew, err := c.Matches.CreateIfNew(db, match)
	if err != nil {
		return nil, false, err
	}
	if !isNew {
		return match, false, nil
	}

	if err := c.Watchers.RecordMatch(db, watcher.ID, match.MatchedAt); err != nil {
		c.Log.WithError(err).Warn("Failed to bump the watcher match counter")
	}

	return match, true, nil
}

// ScheduleEvents writes occurrence 1 for each enabled event. Later occurrences
// are written by the dispatcher as each one settles, so a restart never loses
// a schedule and cancelling is a single update.
func (c *PipelineUseCase) ScheduleEvents(ctx context.Context, watcher *entity.Watcher, match *entity.MatchedEmail) error {
	db := c.DB.WithContext(ctx)

	events, err := c.Events.FindEnabledByWatcher(db, watcher.ID)
	if err != nil {
		return err
	}

	now := time.Now()

	for i := range events {
		event := events[i]

		run := &entity.EventRun{
			ID:             uuid.NewString(),
			UserID:         watcher.UserID,
			MatchedEmailID: match.ID,
			WatcherEventID: event.ID,
			Occurrence:     1,
			Status:         entity.RunStatusPending,
			ScheduledAt:    now.Add(time.Duration(event.DelaySeconds) * time.Second).UnixMilli(),
			MaxAttempts:    3,
			ConfigSnapshot: entity.JSONOrEmpty(event.Config, "{}"),
		}

		if err := c.Runs.Create(db, run); err != nil {
			c.Log.WithError(err).Warnf("Failed to schedule event %s", event.ID)
		}
	}

	return nil
}
