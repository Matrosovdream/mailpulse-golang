//go:build feature

package feature

import (
	"net/http"
	"testing"

	"mailpulse/test/support"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mailAccount struct {
	ID       string         `json:"id"`
	Provider string         `json:"provider"`
	Label    string         `json:"provider_label"`
	Kind     string         `json:"kind"`
	AuthMode string         `json:"auth_mode"`
	Status   string         `json:"status"`
	Settings map[string]any `json:"settings"`
}

func TestConnectMailAccount(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	account := h.Register(t, "connect@example.com", "secret123")
	host, port := h.MailHost()

	t.Run("settings override the provider preset", func(t *testing.T) {
		response := h.Post(t, "/api/mail-accounts", account.Token, map[string]any{
			"provider": "imap", "email_address": "override@corp.com",
			"auth_mode": "password", "username": "demo", "password": "secret123",
			"settings": support.MailAccountSettings(host, port),
		})
		require.Equal(t, http.StatusCreated, response.Status)

		var created mailAccount
		response.Decode(t, &created)

		assert.Equal(t, "pending", created.Status, "a new connection is unverified")
		assert.Equal(t, host, created.Settings["host"])
		assert.Equal(t, float64(port), created.Settings["port"])
		assert.Equal(t, "Generic IMAP", created.Label)
	})

	// picking a named provider should fill the connection in, so the form only
	// has to ask for a password
	t.Run("presets are inherited when settings are omitted", func(t *testing.T) {
		response := h.Post(t, "/api/mail-accounts", account.Token, map[string]any{
			"provider": "yandex", "email_address": "inherited@yandex.com", "password": "app-pw",
		})
		require.Equal(t, http.StatusCreated, response.Status)

		var created mailAccount
		response.Decode(t, &created)

		assert.Equal(t, "imap.yandex.com", created.Settings["host"])
		assert.Equal(t, float64(993), created.Settings["port"])
		assert.Equal(t, true, created.Settings["use_tls"])
		assert.Equal(t, "app_password", created.AuthMode, "the provider's first auth mode is the default")
	})

	t.Run("secrets never come back", func(t *testing.T) {
		listed := h.Get(t, "/api/mail-accounts", account.Token)
		require.Equal(t, http.StatusOK, listed.Status)

		body := string(listed.Body)
		assert.NotContains(t, body, "secret123")
		assert.NotContains(t, body, "app-pw")
		assert.NotContains(t, body, "credentials")
	})

	t.Run("rejects a provider that does not exist", func(t *testing.T) {
		response := h.Post(t, "/api/mail-accounts", account.Token, map[string]any{
			"provider": "nosuchprovider", "email_address": "x@corp.com", "password": "pw",
		})
		assert.Equal(t, http.StatusBadRequest, response.Status)
		assert.Contains(t, response.Error(t), "unknown mail provider")
	})

	t.Run("rejects a provider that is disabled", func(t *testing.T) {
		response := h.Post(t, "/api/mail-accounts", account.Token, map[string]any{
			"provider": "gmail", "email_address": "x@gmail.com", "password": "pw",
		})
		assert.Equal(t, http.StatusBadRequest, response.Status)
		assert.Contains(t, response.Error(t), "not available")
	})

	t.Run("rejects an auth mode the provider does not support", func(t *testing.T) {
		response := h.Post(t, "/api/mail-accounts", account.Token, map[string]any{
			"provider": "yandex", "email_address": "other@yandex.com",
			"auth_mode": "password", "password": "pw",
		})
		assert.Equal(t, http.StatusBadRequest, response.Status)
		assert.Contains(t, response.Error(t), "app_password")
	})

	t.Run("rejects the same mailbox twice", func(t *testing.T) {
		response := h.Post(t, "/api/mail-accounts", account.Token, map[string]any{
			"provider": "imap", "email_address": "override@corp.com",
			"auth_mode": "password", "password": "pw",
			"settings": support.MailAccountSettings(host, port),
		})
		assert.Equal(t, http.StatusConflict, response.Status)
	})
}

func TestVerifyMailAccount(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	account := h.Register(t, "verify@example.com", "secret123")
	host, port := h.MailHost()

	username := h.Config.GetString("test.mail_username")
	password := h.Config.GetString("test.mail_password")

	t.Run("valid credentials verify and list folders", func(t *testing.T) {
		created := h.Post(t, "/api/mail-accounts", account.Token, map[string]any{
			"provider": "imap", "email_address": "good@corp.com",
			"auth_mode": "password", "username": username, "password": password,
			"settings": support.MailAccountSettings(host, port),
		})
		require.Equal(t, http.StatusCreated, created.Status)

		var stored mailAccount
		created.Decode(t, &stored)

		verified := h.Post(t, "/api/mail-accounts/"+stored.ID+"/_verify", account.Token, nil)
		require.Equal(t, http.StatusOK, verified.Status)

		var result struct {
			Status       string `json:"status"`
			FoldersFound int    `json:"folders_found"`
		}
		verified.Decode(t, &result)

		assert.Equal(t, "verified", result.Status)
		assert.Greater(t, result.FoldersFound, 0)

		folders := h.Get(t, "/api/mail-accounts/"+stored.ID+"/folders", account.Token)
		require.Equal(t, http.StatusOK, folders.Status)

		var listed []struct {
			Name string `json:"name"`
		}
		folders.Decode(t, &listed)

		names := make([]string, 0, len(listed))
		for _, folder := range listed {
			names = append(names, folder.Name)
		}
		assert.Contains(t, names, "INBOX")
	})

	// a wrong password must be reported in a way the UI can show next to the
	// field, and must take the account out of the poll queue
	t.Run("bad credentials are reported and disable polling", func(t *testing.T) {
		created := h.Post(t, "/api/mail-accounts", account.Token, map[string]any{
			"provider": "imap", "email_address": "bad@corp.com",
			"auth_mode": "password", "username": username, "password": "definitely-wrong",
			"settings": support.MailAccountSettings(host, port),
		})
		require.Equal(t, http.StatusCreated, created.Status)

		var stored mailAccount
		created.Decode(t, &stored)

		verified := h.Post(t, "/api/mail-accounts/"+stored.ID+"/_verify", account.Token, nil)
		assert.Equal(t, http.StatusUnprocessableEntity, verified.Status)
		assert.Contains(t, verified.Error(t), "login failed")

		detail := h.Get(t, "/api/mail-accounts/"+stored.ID, account.Token)
		var after struct {
			Status    string  `json:"status"`
			LastError *string `json:"last_error"`
		}
		detail.Decode(t, &after)

		assert.Equal(t, "error", after.Status)
		require.NotNil(t, after.LastError)
		assert.Contains(t, *after.LastError, "login failed")
	})
}

// Changing only the password must not wipe the username, which the caller
// cannot re-send because it is never returned.
func TestUpdatePreservesOtherCredentials(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	account := h.Register(t, "merge@example.com", "secret123")
	host, port := h.MailHost()

	username := h.Config.GetString("test.mail_username")
	password := h.Config.GetString("test.mail_password")

	created := h.Post(t, "/api/mail-accounts", account.Token, map[string]any{
		"provider": "imap", "email_address": "merge@corp.com",
		"auth_mode": "password", "username": username, "password": "wrong-at-first",
		"settings": support.MailAccountSettings(host, port),
	})
	require.Equal(t, http.StatusCreated, created.Status)

	var stored mailAccount
	created.Decode(t, &stored)

	// only the password is sent; the username must survive
	updated := h.Patch(t, "/api/mail-accounts/"+stored.ID, account.Token, map[string]any{
		"password": password,
	})
	require.Equal(t, http.StatusOK, updated.Status)

	verified := h.Post(t, "/api/mail-accounts/"+stored.ID+"/_verify", account.Token, nil)
	assert.Equal(t, http.StatusOK, verified.Status,
		"the stored username was lost if this fails")
}

func TestDeleteMailAccountBlockedByWatchers(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	account := h.Register(t, "delete@example.com", "secret123")
	host, port := h.MailHost()

	created := h.Post(t, "/api/mail-accounts", account.Token, map[string]any{
		"provider": "imap", "email_address": "inuse@corp.com",
		"auth_mode": "password", "password": "pw",
		"settings": support.MailAccountSettings(host, port),
	})
	require.Equal(t, http.StatusCreated, created.Status)

	var stored mailAccount
	created.Decode(t, &stored)

	watcher := h.Post(t, "/api/watchers", account.Token, map[string]any{
		"mail_account_id": stored.ID, "name": "Blocking watcher",
	})
	require.Equal(t, http.StatusCreated, watcher.Status)

	// the schema restricts this delete; the API should name what is in the way
	blocked := h.Delete(t, "/api/mail-accounts/"+stored.ID, account.Token)
	assert.Equal(t, http.StatusConflict, blocked.Status)
	assert.Contains(t, blocked.Error(t), "Blocking watcher")

	var created2 struct {
		ID string `json:"id"`
	}
	watcher.Decode(t, &created2)
	require.Equal(t, http.StatusOK, h.Delete(t, "/api/watchers/"+created2.ID, account.Token).Status)

	assert.Equal(t, http.StatusOK, h.Delete(t, "/api/mail-accounts/"+stored.ID, account.Token).Status)
}

func TestOAuthIsNotImplementedYet(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	account := h.Register(t, "oauth@example.com", "secret123")

	response := h.Get(t, "/api/mail-accounts/oauth/gmail/_authorize", account.Token)

	// a clear 501 pointing at the workaround, rather than a confusing failure
	assert.Equal(t, http.StatusNotImplemented, response.Status)
	assert.Contains(t, response.Error(t), "IMAP")
}
