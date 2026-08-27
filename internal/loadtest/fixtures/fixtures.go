// Package fixtures builds a world to measure against.
//
// Rows go in through batched INSERT rather than the usecases, deliberately.
// Creating ten thousand accounts through the HTTP API is itself a load test,
// and one slow enough to discourage running anything else. The tradeoff is
// real — fixtures can drift from what the API would actually produce — so the
// shapes here mirror the usecase defaults, and anything that would be
// encrypted in production is encrypted here too.
package fixtures

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"mailpulse/internal/entity"
	"mailpulse/internal/gateway/secret"
	"mailpulse/internal/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// EmailDomain marks every row a load run creates, so cleanup can be exact
// rather than a truncate that would take the developer's own data with it.
const EmailDomain = "loadtest.invalid"

// batchSize is the insert chunk. Postgres has a 65535 parameter ceiling per
// statement and mail_accounts is the widest row here, so this stays well under
// it rather than being tuned to the edge.
const batchSize = 500

// Shape describes the world to build. Everything multiplies: Users ×
// AccountsPerUser accounts, and that × WatchersPerAccount watchers.
type Shape struct {
	Users              int
	AccountsPerUser    int
	WatchersPerAccount int
	FiltersPerWatcher  int
	EventsPerWatcher   int

	// Provider is a mail_providers slug. The load runs use "imap", because
	// that is the kind the loadstub registers itself under.
	Provider            string
	PollIntervalSeconds int

	// DueNow puts every account's next_poll_at in the past, so a capacity run
	// does not spend its first cycles waiting for work to become due.
	DueNow bool
}

// Built reports what was created, so a suite can address the rows it owns.
type Built struct {
	UserIDs    []string
	AccountIDs []string
	WatcherIDs []string
	// Password is the plaintext behind every user's hash, so the endpoints
	// suite can log one of them in.
	Password string
}

// Accounts is the number actually created, which is the figure a capacity
// result should be reported against.
func (b Built) Accounts() int { return len(b.AccountIDs) }

// Build inserts the shape and returns the ids. It is deterministic for a given
// seed: two runs with the same -seed produce the same world, which is what
// makes two result files comparable.
func Build(ctx context.Context, db *gorm.DB, cipher *secret.Cipher, shape Shape, seed int64) (Built, error) {
	built := Built{Password: "loadtest-password"}
	random := rand.New(rand.NewSource(seed))
	now := time.Now().UnixMilli()

	// One hash, reused. bcrypt is deliberately expensive, so hashing ten
	// thousand of them would make fixture building slower than the test.
	hash, err := hashOnce(built.Password)
	if err != nil {
		return built, err
	}

	credentials, err := cipher.Encrypt(mustJSON(model.MailAccountCredentials{
		Username: "loadtest", Password: "loadtest",
	}))
	if err != nil {
		return built, fmt.Errorf("encrypting fixture credentials: %w", err)
	}

	settings := entity.JSON(mustJSON(map[string]any{
		"host": "loadstub.invalid", "port": 993, "use_tls": true,
	}))

	provider := shape.Provider
	if provider == "" {
		provider = "imap"
	}
	interval := shape.PollIntervalSeconds
	if interval <= 0 {
		interval = 120
	}

	var (
		users    []entity.User
		accounts []entity.MailAccount
		watchers []entity.Watcher
		filters  []entity.WatcherFilter
		events   []entity.WatcherEvent
	)

	for u := 0; u < shape.Users; u++ {
		userID := uuid.NewString()
		built.UserIDs = append(built.UserIDs, userID)

		users = append(users, entity.User{
			ID:              userID,
			Email:           fmt.Sprintf("user-%06d@%s", u, EmailDomain),
			Name:            fmt.Sprintf("Load User %06d", u),
			Password:        hash,
			Status:          entity.UserStatusActive,
			Timezone:        "UTC",
			EmailVerifiedAt: &now,
		})

		for a := 0; a < shape.AccountsPerUser; a++ {
			accountID := uuid.NewString()
			built.AccountIDs = append(built.AccountIDs, accountID)

			// spread next_poll_at across the interval unless the suite wants
			// everything due at once; a real fleet is never in lockstep
			next := now
			if !shape.DueNow {
				next = now + int64(random.Intn(interval*1000))
			} else {
				next = now - int64(random.Intn(60_000))
			}

			accounts = append(accounts, entity.MailAccount{
				ID:                  accountID,
				UserID:              userID,
				Provider:            provider,
				EmailAddress:        fmt.Sprintf("mailbox-%06d-%02d@%s", u, a, EmailDomain),
				AuthMode:            "password",
				Credentials:         credentials,
				Settings:            settings,
				Status:              entity.MailAccountStatusVerified,
				SyncState:           entity.JSON("{}"),
				PollIntervalSeconds: interval,
				NextPollAt:          next,
			})

			for w := 0; w < shape.WatchersPerAccount; w++ {
				watcherID := uuid.NewString()
				built.WatcherIDs = append(built.WatcherIDs, watcherID)

				watchers = append(watchers, entity.Watcher{
					ID:            watcherID,
					UserID:        userID,
					MailAccountID: accountID,
					Name:          fmt.Sprintf("watcher %d/%d/%d", u, a, w),
					Status:        entity.WatcherStatusActive,
					MatchMode:     entity.MatchModeAll,
					Folder:        "INBOX",
				})

				for f := 0; f < shape.FiltersPerWatcher; f++ {
					filters = append(filters, entity.WatcherFilter{
						ID:        uuid.NewString(),
						WatcherID: watcherID,
						Field:     filterFields[f%len(filterFields)],
						Operator:  entity.OpContains,
						Value:     filterValues[f%len(filterValues)],
						Position:  f,
					})
				}

				for e := 0; e < shape.EventsPerWatcher; e++ {
					events = append(events, entity.WatcherEvent{
						ID:        uuid.NewString(),
						WatcherID: watcherID,
						Type:      "notify",
						Config:    entity.JSON(`{"message":"load"}`),
						Position:  e,
						Enabled:   true,
						RunMode:   entity.RunModeImmediate,
					})
				}
			}
		}
	}

	// order matters: every table below points at the one above it
	inserts := []struct {
		what string
		rows any
	}{
		{"users", users},
		{"mail_accounts", accounts},
		{"watchers", watchers},
		{"watcher_filters", filters},
		{"watcher_events", events},
	}

	for _, insert := range inserts {
		if err := createInBatches(ctx, db, insert.rows); err != nil {
			return built, fmt.Errorf("inserting %s: %w", insert.what, err)
		}
	}

	return built, nil
}

// filterFields and filterValues keep generated filters plausible: a filter
// that never matches makes the matcher's cheap path the only one measured.
var (
	filterFields = []string{entity.FieldSubject, entity.FieldFrom, entity.FieldBody, entity.FieldTo}
	filterValues = []string{"invoice", "alert", "load", "@" + EmailDomain}
)

func createInBatches(ctx context.Context, db *gorm.DB, rows any) error {
	switch typed := rows.(type) {
	case []entity.User:
		if len(typed) == 0 {
			return nil
		}
		return db.WithContext(ctx).CreateInBatches(typed, batchSize).Error
	case []entity.MailAccount:
		if len(typed) == 0 {
			return nil
		}
		return db.WithContext(ctx).CreateInBatches(typed, batchSize).Error
	case []entity.Watcher:
		if len(typed) == 0 {
			return nil
		}
		return db.WithContext(ctx).CreateInBatches(typed, batchSize).Error
	case []entity.WatcherFilter:
		if len(typed) == 0 {
			return nil
		}
		return db.WithContext(ctx).CreateInBatches(typed, batchSize).Error
	case []entity.WatcherEvent:
		if len(typed) == 0 {
			return nil
		}
		return db.WithContext(ctx).CreateInBatches(typed, batchSize).Error
	}
	return fmt.Errorf("fixtures: no batch writer for %T", rows)
}

func mustJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err) // fixture shapes are literals; a failure here is a bug, not input
	}
	return string(encoded)
}
