//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"testing"

	"mailpulse/internal/entity"
	"mailpulse/internal/seeder"
	"mailpulse/test/support"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Seeders must be safe to run repeatedly: a re-run on deploy is normal, and it
// must not duplicate rows or reset a password someone changed.
func TestUserSeederIsIdempotent(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	raw := json.RawMessage(`[
		{"email":"seeded@example.com","name":"Seeded","password":"123","roles":["user","superadmin"]}
	]`)

	userSeeder := seeder.NewUserSeeder()

	run := func(t *testing.T) seeder.Result {
		t.Helper()

		var result seeder.Result
		err := h.DB.Transaction(func(tx *gorm.DB) error {
			var txErr error
			result, txErr = userSeeder.Seed(context.Background(), tx, raw)
			return txErr
		})
		require.NoError(t, err)
		return result
	}

	first := run(t)
	assert.Equal(t, 1, first.Created)

	second := run(t)
	assert.Equal(t, 0, second.Created)
	assert.Equal(t, 1, second.Skipped, "a second run changes nothing")

	var total int64
	require.NoError(t, h.DB.Model(&entity.User{}).
		Where("email = ?", "seeded@example.com").Count(&total).Error)
	assert.Equal(t, int64(1), total)

	t.Run("roles are attached", func(t *testing.T) {
		var slugs []string
		require.NoError(t, h.DB.Table("roles").
			Select("roles.slug").
			Joins("JOIN user_roles ON user_roles.role_id = roles.id").
			Joins("JOIN users ON users.id = user_roles.user_id").
			Where("users.email = ?", "seeded@example.com").
			Order("roles.slug").
			Scan(&slugs).Error)

		assert.Equal(t, []string{"superadmin", "user"}, slugs)
	})

	// the password is hashed on the way in, never stored as written
	t.Run("password is hashed", func(t *testing.T) {
		var stored entity.User
		require.NoError(t, h.DB.Where("email = ?", "seeded@example.com").Take(&stored).Error)

		assert.NotEqual(t, "123", stored.Password)
		assert.Contains(t, stored.Password, "$2a$")
	})

	// an operator changing a password must not have it reverted by a re-seed
	t.Run("an existing password is never overwritten", func(t *testing.T) {
		require.NoError(t, h.DB.Model(&entity.User{}).
			Where("email = ?", "seeded@example.com").
			Update("password", "changed-by-hand").Error)

		run(t)

		var stored entity.User
		require.NoError(t, h.DB.Where("email = ?", "seeded@example.com").Take(&stored).Error)
		assert.Equal(t, "changed-by-hand", stored.Password)
	})
}

func TestUserSeederRejectsBadInput(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	userSeeder := seeder.NewUserSeeder()

	cases := map[string]string{
		"not a list":       `{"email":"x@example.com"}`,
		"missing email":    `[{"name":"X","password":"123"}]`,
		"missing password": `[{"email":"x@example.com","name":"X"}]`,
		"unknown role":     `[{"email":"x@example.com","password":"123","roles":["wizard"]}]`,
	}

	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			err := h.DB.Transaction(func(tx *gorm.DB) error {
				_, err := userSeeder.Seed(context.Background(), tx, json.RawMessage(payload))
				return err
			})
			assert.Error(t, err)
		})
	}
}

func TestMailProviderSeederUpserts(t *testing.T) {
	h := support.New(t)

	providerSeeder := seeder.NewMailProviderSeeder()

	// the migration already seeded these, so an unchanged file is a no-op
	raw := json.RawMessage(`[
		{"slug":"yandex","label":"Yandex Mail","kind":"imap","auth_modes":"app_password",
		 "default_host":"imap.yandex.com","default_port":993,"default_use_tls":true,
		 "help_url":"https://yandex.com/support/mail/mail-clients/others.html",
		 "enabled":true,"position":1}
	]`)

	var result seeder.Result
	require.NoError(t, h.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = providerSeeder.Seed(context.Background(), tx, raw)
		return err
	}))
	assert.Equal(t, 1, result.Skipped, "an identical row is left alone")

	// changing a value in the file updates the row, which is the point of
	// making providers data rather than a migration
	changed := json.RawMessage(`[
		{"slug":"yandex","label":"Yandex 360","kind":"imap","auth_modes":"app_password",
		 "default_host":"imap.yandex.com","default_port":993,"default_use_tls":true,
		 "help_url":"https://yandex.com/support/mail/mail-clients/others.html",
		 "enabled":true,"position":1}
	]`)

	require.NoError(t, h.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = providerSeeder.Seed(context.Background(), tx, changed)
		return err
	}))
	assert.Equal(t, 1, result.Updated)

	var stored entity.MailProvider
	require.NoError(t, h.DB.Where("slug = ?", "yandex").Take(&stored).Error)
	assert.Equal(t, "Yandex 360", stored.Label)

	// put it back so later tests see the seeded value
	require.NoError(t, h.DB.Transaction(func(tx *gorm.DB) error {
		_, err := providerSeeder.Seed(context.Background(), tx, raw)
		return err
	}))
}
