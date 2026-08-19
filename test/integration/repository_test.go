//go:build integration

package integration

import (
	"testing"
	"time"

	"mailpulse/internal/entity"
	"mailpulse/internal/repository"
	"mailpulse/test/support"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newUser(t *testing.T, db *gorm.DB, email string) *entity.User {
	t.Helper()

	user := &entity.User{
		ID: uuid.NewString(), Email: email, Name: email,
		Password: "hash", Status: entity.UserStatusActive, Timezone: "UTC",
	}
	require.NoError(t, db.Create(user).Error)

	return user
}

// newMailAccount builds a valid row: status and the jsonb columns are not
// nullable, so a fixture that omits them trips a check constraint rather than
// testing anything.
func newMailAccount(t *testing.T, db *gorm.DB, userID, address string) *entity.MailAccount {
	t.Helper()

	account := &entity.MailAccount{
		ID: uuid.NewString(), UserID: userID, Provider: "imap",
		EmailAddress: address, AuthMode: "password", Credentials: "enc",
		Settings: entity.JSON(`{}`), SyncState: entity.JSON(`{}`),
		Status: entity.MailAccountStatusVerified, PollIntervalSeconds: 120,
	}
	require.NoError(t, db.Create(account).Error)

	return account
}

func newWatcher(t *testing.T, db *gorm.DB, userID, accountID, name string) *entity.Watcher {
	t.Helper()

	watcher := &entity.Watcher{
		ID: uuid.NewString(), UserID: userID, MailAccountID: accountID,
		Name: name, Status: entity.WatcherStatusActive,
		MatchMode: entity.MatchModeAll, Folder: "INBOX",
	}
	require.NoError(t, db.Create(watcher).Error)

	return watcher
}

func TestUserRepository_Roles(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	repo := repository.NewUserRepository(logrus.New())
	roleRepo := repository.NewRoleRepository(logrus.New())
	user := newUser(t, h.DB, "roles@example.com")

	var role entity.Role
	require.NoError(t, h.DB.Where("slug = ?", entity.RoleUser).Take(&role).Error)
	require.NoError(t, repo.AddRole(h.DB, user.ID, role.ID))

	require.NoError(t, repo.LoadRoles(h.DB, user))
	assert.Equal(t, []string{entity.RoleUser}, user.RoleSlugs())

	t.Run("ReplaceRoles swaps the whole set", func(t *testing.T) {
		roles, err := roleRepo.FindBySlugs(h.DB, []string{entity.RoleSuperadmin})
		require.NoError(t, err)
		require.Len(t, roles, 1)

		require.NoError(t, repo.ReplaceRoles(h.DB, user.ID, []string{roles[0].ID}))
		require.NoError(t, repo.LoadRoles(h.DB, user))

		assert.Equal(t, []string{entity.RoleSuperadmin}, user.RoleSlugs())
	})

	// the admin list would otherwise issue one query per row
	t.Run("LoadRolesForAll avoids the N+1", func(t *testing.T) {
		second := newUser(t, h.DB, "roles2@example.com")

		var plain entity.Role
		require.NoError(t, h.DB.Where("slug = ?", entity.RoleUser).Take(&plain).Error)
		require.NoError(t, repo.AddRole(h.DB, second.ID, plain.ID))

		users := []entity.User{*user, *second}
		require.NoError(t, repo.LoadRolesForAll(h.DB, users))

		assert.Equal(t, []string{entity.RoleSuperadmin}, users[0].RoleSlugs())
		assert.Equal(t, []string{entity.RoleUser}, users[1].RoleSlugs())
	})
}

// The unique (watcher_id, message_id) index is what stops a re-read mailbox
// firing the same watcher twice, so it is worth testing directly.
func TestMatchedEmailRepository_Dedupe(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	repo := repository.NewMatchedEmailRepository(logrus.New())
	user := newUser(t, h.DB, "dedupe@example.com")
	account := newMailAccount(t, h.DB, user.ID, "dedupe@corp.com")
	watcher := newWatcher(t, h.DB, user.ID, account.ID, "Dedupe")

	build := func(watcherID string) *entity.MatchedEmail {
		return &entity.MatchedEmail{
			ID: uuid.NewString(), UserID: user.ID, WatcherID: watcherID,
			MailAccountID: account.ID, MessageID: "<same@corp.com>", ProviderUID: "1",
			ReceivedAt: time.Now().UnixMilli(), MatchedAt: time.Now().UnixMilli(),
			MatchedFilters: entity.JSON(`[]`),
		}
	}

	created, err := repo.CreateIfNew(h.DB, build(watcher.ID))
	require.NoError(t, err)
	assert.True(t, created, "the first sighting is new")

	// the same message id again is a silent no-op, not an error
	created, err = repo.CreateIfNew(h.DB, build(watcher.ID))
	require.NoError(t, err)
	assert.False(t, created)

	total, err := repo.CountForWatcher(h.DB, watcher.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)

	// a different watcher may match the same message: the index is per watcher
	other := newWatcher(t, h.DB, user.ID, account.ID, "Other")

	created, err = repo.CreateIfNew(h.DB, build(other.ID))
	require.NoError(t, err)
	assert.True(t, created)
}

func TestMailProviderRepository(t *testing.T) {
	h := support.New(t)

	repo := repository.NewMailProviderRepository(logrus.New())

	enabled, err := repo.FindEnabled(h.DB)
	require.NoError(t, err)
	require.NotEmpty(t, enabled)

	for _, provider := range enabled {
		assert.True(t, provider.Enabled)
	}

	all, err := repo.FindAll(h.DB)
	require.NoError(t, err)
	assert.Greater(t, len(all), len(enabled), "gmail and outlook are seeded disabled")

	var yandex entity.MailProvider
	require.NoError(t, repo.FindBySlug(h.DB, &yandex, "yandex"))
	assert.Equal(t, "imap", yandex.Kind)
	require.NotNil(t, yandex.DefaultHost)
	assert.Equal(t, "imap.yandex.com", *yandex.DefaultHost)
	assert.True(t, yandex.SupportsAuthMode("app_password"))
}

// ClaimDue is the dispatcher's queue read; CancelPendingAfter is what stops a
// repeat chain. Both are worth exercising against real SQL.
func TestEventRunRepository_Queue(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	runs := repository.NewEventRunRepository(logrus.New())
	user := newUser(t, h.DB, "queue@example.com")
	account := newMailAccount(t, h.DB, user.ID, "queue@corp.com")
	watcher := newWatcher(t, h.DB, user.ID, account.ID, "Queue")

	watcherEvent := &entity.WatcherEvent{
		ID: uuid.NewString(), WatcherID: watcher.ID, Type: "notify",
		Config: entity.JSON(`{}`), Enabled: true, RunMode: entity.RunModeImmediate,
	}
	require.NoError(t, h.DB.Create(watcherEvent).Error)

	match := &entity.MatchedEmail{
		ID: uuid.NewString(), UserID: user.ID, WatcherID: watcher.ID,
		MailAccountID: account.ID, MessageID: "<queue@corp.com>", ProviderUID: "1",
		ReceivedAt: time.Now().UnixMilli(), MatchedAt: time.Now().UnixMilli(),
		MatchedFilters: entity.JSON(`[]`),
	}
	require.NoError(t, h.DB.Create(match).Error)

	now := time.Now().UnixMilli()
	schedule := map[int]int64{1: now - 1000, 2: now + 600_000, 3: now + 1_200_000}
	for occurrence, scheduledAt := range schedule {
		require.NoError(t, h.DB.Create(&entity.EventRun{
			ID: uuid.NewString(), UserID: user.ID, MatchedEmailID: match.ID,
			WatcherEventID: watcherEvent.ID, Occurrence: occurrence,
			Status: entity.RunStatusPending, ScheduledAt: scheduledAt,
			MaxAttempts: 3, ConfigSnapshot: entity.JSON(`{}`),
		}).Error)
	}

	t.Run("only due runs are claimed", func(t *testing.T) {
		require.NoError(t, h.DB.Transaction(func(tx *gorm.DB) error {
			claimed, err := runs.ClaimDue(tx, now, 10)
			require.NoError(t, err)
			require.Len(t, claimed, 1, "the two future occurrences are not due yet")
			assert.Equal(t, 1, claimed[0].Occurrence)
			return nil
		}))
	})

	t.Run("cancelling stops everything scheduled after it", func(t *testing.T) {
		cancelled, err := runs.CancelPendingAfter(h.DB, match.ID, watcherEvent.ID, 2)
		require.NoError(t, err)
		assert.Equal(t, int64(2), cancelled)

		var remaining int64
		require.NoError(t, h.DB.Model(&entity.EventRun{}).
			Where("status = ?", entity.RunStatusPending).Count(&remaining).Error)
		assert.Equal(t, int64(1), remaining, "occurrence 1 was due, not cancelled")
	})
}

func TestWatcherRepository_Search(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	repo := repository.NewWatcherRepository(logrus.New())
	user := newUser(t, h.DB, "search@example.com")
	account := newMailAccount(t, h.DB, user.ID, "search@corp.com")

	for name, status := range map[string]string{
		"Active one": entity.WatcherStatusActive,
		"Paused one": entity.WatcherStatusPaused,
		"Archived":   entity.WatcherStatusArchived,
	} {
		watcher := newWatcher(t, h.DB, user.ID, account.ID, name)
		require.NoError(t, h.DB.Model(watcher).Update("status", status).Error)
	}

	count := func(status string) int {
		var found []entity.Watcher
		_, err := repo.Paginate(repo.Search(h.DB, user.ID, status, "", ""), &found, 1, 50)
		require.NoError(t, err)
		return len(found)
	}

	assert.Equal(t, 1, count(entity.WatcherStatusActive))
	assert.Equal(t, 1, count(entity.WatcherStatusArchived))
	assert.Equal(t, 3, count("all"), `"all" is not a status, it means no filter`)
	assert.Equal(t, 3, count(""))

	counts, err := repo.CountByStatusForUser(h.DB, user.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(3), counts["total"])
	assert.Equal(t, int64(1), counts[entity.WatcherStatusPaused])
}
