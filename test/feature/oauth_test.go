//go:build feature

package feature

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"mailpulse/internal/entity"
	"mailpulse/internal/gateway/secret"
	"mailpulse/internal/model"
	"mailpulse/test/support"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type authorizeResponse struct {
	RedirectURL string `json:"redirect_url"`
	State       string `json:"state"`
}

// stateOf pulls the state parameter back out of the consent URL, which is the
// only place a real client would find it.
func stateOf(t *testing.T, consentURL string) string {
	t.Helper()

	parsed, err := url.Parse(consentURL)
	require.NoError(t, err)

	state := parsed.Query().Get("state")
	require.NotEmpty(t, state, "the consent URL carries no state: %s", consentURL)

	return state
}

// callbackPath is the URL the provider redirects the browser to.
func callbackPath(provider, code, state string) string {
	query := url.Values{"code": {code}, "state": {state}}
	return "/api/mail-accounts/oauth/" + provider + "/_callback?" + query.Encode()
}

// oauthError reads the reason off a callback redirect. Empty means success.
func oauthError(t *testing.T, location string) string {
	t.Helper()

	parsed, err := url.Parse(location)
	require.NoError(t, err)

	return parsed.Query().Get("oauth_error")
}

// accountRow reads the stored row directly, because the parts that matter most
// here — the encrypted blob, the expiry, the scopes — are deliberately not all
// in the API response.
func accountRow(t *testing.T, h *support.Harness, email string) *entity.MailAccount {
	t.Helper()

	account := new(entity.MailAccount)
	err := h.DB.Where("email_address = ?", email).Take(account).Error
	require.NoError(t, err, "no mail account was stored for %s", email)

	return account
}

func decryptCredentials(t *testing.T, h *support.Harness, account *entity.MailAccount) model.MailAccountCredentials {
	t.Helper()

	cipher, err := secret.NewCipher(h.Config.GetString("security.encryption_key"))
	require.NoError(t, err)

	plaintext, err := cipher.Decrypt(account.Credentials)
	require.NoError(t, err)

	var credentials model.MailAccountCredentials
	require.NoError(t, json.Unmarshal([]byte(plaintext), &credentials))

	return credentials
}

func TestOAuthAuthorize(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	account := h.Register(t, "oauth-authorize@example.com", "secret123")

	t.Run("needs a session", func(t *testing.T) {
		response := h.Get(t, "/api/mail-accounts/oauth/yandex/_authorize", "")

		assert.Equal(t, http.StatusUnauthorized, response.Status,
			"starting a connect flow decides whose mailbox it lands on")
	})

	t.Run("returns a consent url and remembers the state", func(t *testing.T) {
		response := h.Get(t, "/api/mail-accounts/oauth/yandex/_authorize", account.Token)
		require.Equal(t, http.StatusOK, response.Status, response.Error(t))

		var authorize authorizeResponse
		response.Decode(t, &authorize)

		assert.Equal(t, authorize.State, stateOf(t, authorize.RedirectURL),
			"the state in the URL and the one handed to the client have to be the same value")

		exists, err := h.Redis.Exists(context.Background(), "oauth:state:"+authorize.State).Result()
		require.NoError(t, err)
		assert.Equal(t, int64(1), exists, "the callback has no session, so the state is the only way back")
	})

	t.Run("every call mints a fresh state", func(t *testing.T) {
		first := h.Get(t, "/api/mail-accounts/oauth/yandex/_authorize", account.Token)
		second := h.Get(t, "/api/mail-accounts/oauth/yandex/_authorize", account.Token)

		var a, b authorizeResponse
		first.Decode(t, &a)
		second.Decode(t, &b)

		assert.NotEqual(t, a.State, b.State, "a reused state would be replayable across flows")
	})

	t.Run("refuses a provider that does not do oauth", func(t *testing.T) {
		response := h.Get(t, "/api/mail-accounts/oauth/imap/_authorize", account.Token)

		assert.Equal(t, http.StatusBadRequest, response.Status)
		assert.Contains(t, response.Error(t), "does not connect over OAuth")
	})

	t.Run("says so when the provider is not enabled", func(t *testing.T) {
		// gmail ships disabled until an application is registered with Google
		response := h.Get(t, "/api/mail-accounts/oauth/gmail/_authorize", account.Token)

		assert.Equal(t, http.StatusBadRequest, response.Status)
		assert.Contains(t, response.Error(t), "not available yet")
	})

	// A provider that could do OAuth but has no client id configured must say
	// so and point at the app-password route, rather than sending the user to a
	// consent screen that would reject them.
	t.Run("says so when no client id is configured", func(t *testing.T) {
		require.NoError(t, h.DB.Exec(
			"UPDATE mail_providers SET auth_modes = 'app_password,xoauth2' WHERE slug = 'mailru'").Error)
		t.Cleanup(func() {
			h.DB.Exec("UPDATE mail_providers SET auth_modes = 'app_password' WHERE slug = 'mailru'")
		})

		response := h.Get(t, "/api/mail-accounts/oauth/mailru/_authorize", account.Token)

		assert.Equal(t, http.StatusNotImplemented, response.Status)
		assert.Contains(t, response.Error(t), "not configured yet")
		assert.Contains(t, response.Error(t), "IMAP")
	})
}

func TestOAuthCallbackConnectsTheMailbox(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	user := h.Register(t, "oauth-callback@example.com", "secret123")

	authorize := h.Get(t, "/api/mail-accounts/oauth/yandex/_authorize", user.Token)
	require.Equal(t, http.StatusOK, authorize.Status, authorize.Error(t))

	var started authorizeResponse
	authorize.Decode(t, &started)

	response := h.Get(t, callbackPath("yandex", "the-code", started.State), "")

	// the caller here is a browser following the provider's redirect, so the
	// answer is a redirect, not a JSON body nobody would read
	require.Equal(t, http.StatusFound, response.Status)
	assert.Empty(t, oauthError(t, response.Location()), "the callback reported: %s", response.Location())
	assert.Contains(t, response.Location(), "/mail-accounts?connected=")

	stored := accountRow(t, h, "connected@yandex.com")

	assert.Equal(t, "yandex", stored.Provider)
	assert.Equal(t, "xoauth2", stored.AuthMode)
	assert.Equal(t, entity.MailAccountStatusPending, stored.Status,
		"consent proves the user agreed, not that the mailbox opens; the first verify decides that")
	require.NotNil(t, stored.TokenExpiresAt)
	assert.Greater(t, *stored.TokenExpiresAt, time.Now().UnixMilli(),
		"an expiry in the past would make every poll refresh")
	require.NotNil(t, stored.Scopes)
	assert.Contains(t, *stored.Scopes, "mail:imap_ro")
	require.NotNil(t, stored.ProviderAccountID)
	assert.Equal(t, "stub-subject-1", *stored.ProviderAccountID)

	// the settings come from the provider preset, because there was no form
	var settings map[string]any
	require.NoError(t, json.Unmarshal(stored.Settings, &settings))
	assert.Equal(t, "imap.yandex.com", settings["host"])
	assert.Equal(t, float64(993), settings["port"])

	credentials := decryptCredentials(t, h, stored)
	assert.Equal(t, "stub-access-token", credentials.AccessToken)
	assert.Equal(t, "stub-refresh-token", credentials.RefreshToken)

	t.Run("the tokens are never returned to the client", func(t *testing.T) {
		got := h.Get(t, "/api/mail-accounts/"+stored.ID, user.Token)
		require.Equal(t, http.StatusOK, got.Status)

		assert.NotContains(t, string(got.Body), "stub-access-token")
		assert.NotContains(t, string(got.Body), "stub-refresh-token")
	})

	t.Run("it belongs to the user who started the flow", func(t *testing.T) {
		other := h.Register(t, "someone-else@example.com", "secret123")

		got := h.Get(t, "/api/mail-accounts/"+stored.ID, other.Token)
		assert.Equal(t, http.StatusNotFound, got.Status)
	})
}

func TestOAuthCallbackRejections(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	user := h.Register(t, "oauth-rejections@example.com", "secret123")

	countAccounts := func(t *testing.T) int64 {
		t.Helper()
		var total int64
		require.NoError(t, h.DB.Model(&entity.MailAccount{}).Count(&total).Error)
		return total
	}

	t.Run("an unknown state is refused", func(t *testing.T) {
		response := h.Get(t, callbackPath("yandex", "the-code", "never-issued"), "")

		require.Equal(t, http.StatusFound, response.Status)
		assert.Equal(t, "state", oauthError(t, response.Location()))
		assert.Zero(t, countAccounts(t))
	})

	t.Run("a missing code is refused", func(t *testing.T) {
		response := h.Get(t, "/api/mail-accounts/oauth/yandex/_callback?state=something", "")

		require.Equal(t, http.StatusFound, response.Status)
		assert.Equal(t, "invalid", oauthError(t, response.Location()))
	})

	t.Run("a refusal at the consent screen is reported as such", func(t *testing.T) {
		response := h.Get(t, "/api/mail-accounts/oauth/yandex/_callback?error=access_denied", "")

		require.Equal(t, http.StatusFound, response.Status)
		assert.Equal(t, "denied", oauthError(t, response.Location()),
			"declining is a decision, not a fault, and the UI should say something different")
	})

	// A state issued for one provider must not be spendable at another: the
	// callback route is public, and the provider segment is caller-controlled.
	t.Run("a state cannot be spent on a different provider", func(t *testing.T) {
		authorize := h.Get(t, "/api/mail-accounts/oauth/yandex/_authorize", user.Token)
		var started authorizeResponse
		authorize.Decode(t, &started)

		response := h.Get(t, callbackPath("gmail", "the-code", started.State), "")

		require.Equal(t, http.StatusFound, response.Status)
		assert.Equal(t, "state", oauthError(t, response.Location()))
		assert.Zero(t, countAccounts(t))
	})

	// Callback URLs sit in browser history, server logs and referrer headers,
	// so they get replayed. The second one must not connect anything.
	t.Run("a replayed callback is refused", func(t *testing.T) {
		h.Reset(t)
		user := h.Register(t, "oauth-replay@example.com", "secret123")

		authorize := h.Get(t, "/api/mail-accounts/oauth/yandex/_authorize", user.Token)
		var started authorizeResponse
		authorize.Decode(t, &started)

		path := callbackPath("yandex", "the-code", started.State)

		first := h.Get(t, path, "")
		require.Equal(t, http.StatusFound, first.Status)
		require.Empty(t, oauthError(t, first.Location()))

		second := h.Get(t, path, "")
		require.Equal(t, http.StatusFound, second.Status)
		assert.Equal(t, "state", oauthError(t, second.Location()))

		assert.Equal(t, int64(1), countAccounts(t), "the replay created a second mailbox")
	})

	t.Run("a failed exchange does not connect anything", func(t *testing.T) {
		h.Reset(t)
		user := h.Register(t, "oauth-exchange-fails@example.com", "secret123")

		h.OAuth.Configure(func(c *support.StubConfig) { c.TokenStatus = http.StatusBadRequest })

		authorize := h.Get(t, "/api/mail-accounts/oauth/yandex/_authorize", user.Token)
		var started authorizeResponse
		authorize.Decode(t, &started)

		response := h.Get(t, callbackPath("yandex", "the-code", started.State), "")

		require.Equal(t, http.StatusFound, response.Status)
		assert.Equal(t, "exchange", oauthError(t, response.Location()))
		assert.Zero(t, countAccounts(t))

		// the provider's own words must not reach the browser: that text is
		// attacker-influenced and would be reflected into the page
		assert.NotContains(t, response.Location(), "invalid_grant")
		assert.NotContains(t, response.Location(), "revoked")
	})
}

// An OAuth-connected mailbox has to authenticate with the token, not fall
// through to LOGIN with an empty password.
//
// GreenMail 2.1.0 advertises no SASL mechanisms at all, so the XOAUTH2
// handshake cannot be completed against it and this does not claim to. What it
// does prove, against a real IMAP server, is that connect() takes the token
// branch: the failure is a rejected AUTHENTICATE, not a rejected LOGIN. The
// mechanism's own bytes are covered by the unit tests in
// internal/gateway/mail/imap.
func TestXOAuth2IsUsedForOAuthAccounts(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	user := h.Register(t, "oauth-imap@example.com", "secret123")
	host, port := h.MailHost()

	authorize := h.Get(t, "/api/mail-accounts/oauth/yandex/_authorize", user.Token)
	require.Equal(t, http.StatusOK, authorize.Status, authorize.Error(t))

	var started authorizeResponse
	authorize.Decode(t, &started)

	callback := h.Get(t, callbackPath("yandex", "the-code", started.State), "")
	require.Empty(t, oauthError(t, callback.Location()))

	stored := accountRow(t, h, "connected@yandex.com")

	// point it at the local server instead of imap.yandex.com, so the test
	// talks to something it controls
	patched := h.Patch(t, "/api/mail-accounts/"+stored.ID, user.Token, map[string]any{
		"settings": support.MailAccountSettings(host, port),
	})
	require.Equal(t, http.StatusOK, patched.Status, patched.Error(t))

	t.Run("the token branch is taken, not LOGIN", func(t *testing.T) {
		response := h.Post(t, "/api/mail-accounts/"+stored.ID+"/_verify", user.Token, nil)

		require.Equal(t, http.StatusUnprocessableEntity, response.Status)

		message := response.Error(t)
		assert.Contains(t, message, "token authentication failed",
			"an oauth account must not be sent down the password path")
		assert.NotContains(t, message, "login failed",
			"falling through to LOGIN would send an empty password to the server")
	})

	// An account whose token went missing has to say so rather than attempting
	// "auth=Bearer " and getting back a generic authentication failure.
	t.Run("a missing access token is named", func(t *testing.T) {
		require.NoError(t, h.DB.Model(&entity.MailAccount{}).
			Where("id = ?", stored.ID).
			Update("credentials", "").Error)

		response := h.Post(t, "/api/mail-accounts/"+stored.ID+"/_verify", user.Token, nil)

		require.Equal(t, http.StatusUnprocessableEntity, response.Status)
		assert.Contains(t, response.Error(t), "no access token stored")
		assert.Contains(t, response.Error(t), "reconnect")
	})
}

// connectOAuthAccount runs the whole consent flow and hands back the stored
// row, pointed at the local mail server.
func connectOAuthAccount(t *testing.T, h *support.Harness, user support.Account) *entity.MailAccount {
	t.Helper()

	authorize := h.Get(t, "/api/mail-accounts/oauth/yandex/_authorize", user.Token)
	require.Equal(t, http.StatusOK, authorize.Status, authorize.Error(t))

	var started authorizeResponse
	authorize.Decode(t, &started)

	callback := h.Get(t, callbackPath("yandex", "the-code", started.State), "")
	require.Empty(t, oauthError(t, callback.Location()))

	stored := accountRow(t, h, "connected@yandex.com")

	host, port := h.MailHost()
	patched := h.Patch(t, "/api/mail-accounts/"+stored.ID, user.Token, map[string]any{
		"settings": support.MailAccountSettings(host, port),
	})
	require.Equal(t, http.StatusOK, patched.Status, patched.Error(t))

	return accountRow(t, h, "connected@yandex.com")
}

// expireToken backdates the stored expiry so the next use has to renew.
func expireToken(t *testing.T, h *support.Harness, id string) {
	t.Helper()

	require.NoError(t, h.DB.Model(&entity.MailAccount{}).
		Where("id = ?", id).
		Update("token_expires_at", time.Now().Add(-time.Hour).UnixMilli()).Error)
}

func TestOAuthTokenRefresh(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	user := h.Register(t, "oauth-refresh@example.com", "secret123")

	stored := connectOAuthAccount(t, h, user)

	_, refreshesAfterConnect := h.OAuth.Counts()
	require.Zero(t, refreshesAfterConnect, "connecting should not have refreshed anything")

	h.OAuth.Configure(func(c *support.StubConfig) { c.AccessToken = "refreshed-access-token" })
	expireToken(t, h, stored.ID)

	// _verify opens the mailbox, so it goes through the resolver. It fails at
	// the IMAP handshake against GreenMail, which is fine: the refresh happens
	// before the connection is attempted, and that is what is under test.
	h.Post(t, "/api/mail-accounts/"+stored.ID+"/_verify", user.Token, nil)

	_, refreshes := h.OAuth.Counts()
	assert.Equal(t, 1, refreshes, "an expiring token should have been renewed before use")

	renewed := accountRow(t, h, "connected@yandex.com")

	// The renewal is committed on its own handle rather than the caller's
	// transaction. Providers retire the old refresh token the moment they issue
	// a new one, so a renewal rolled back with the caller's work would leave the
	// mailbox holding a credential the provider has already thrown away.
	// TestOAuthRevokedGrantParksTheAccount is the case that actually exercises
	// a rollback; this one just checks the write lands.
	credentials := decryptCredentials(t, h, renewed)
	assert.Equal(t, "refreshed-access-token", credentials.AccessToken)

	require.NotNil(t, renewed.TokenExpiresAt)
	assert.Greater(t, *renewed.TokenExpiresAt, time.Now().UnixMilli())

	t.Run("a token that is still good is not renewed again", func(t *testing.T) {
		h.Post(t, "/api/mail-accounts/"+stored.ID+"/_verify", user.Token, nil)

		_, again := h.OAuth.Counts()
		assert.Equal(t, 1, again,
			"a thirty second poll interval turns an unconditional refresh into a storm")
	})
}

// A user who withdraws consent has to park the mailbox, not be retried forever:
// every retry is a request the provider counts against us, and no number of
// them will succeed.
func TestOAuthRevokedGrantParksTheAccount(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	user := h.Register(t, "oauth-revoked@example.com", "secret123")

	stored := connectOAuthAccount(t, h, user)

	h.OAuth.Configure(func(c *support.StubConfig) { c.TokenStatus = http.StatusBadRequest })
	expireToken(t, h, stored.ID)

	response := h.Post(t, "/api/mail-accounts/"+stored.ID+"/_verify", user.Token, nil)
	assert.Equal(t, http.StatusFailedDependency, response.Status)

	parked := accountRow(t, h, "connected@yandex.com")

	// _verify rolls its transaction back when the resolver fails, so parking
	// the account only survives because the resolver commits it on its own
	// handle. Routed through the caller's transaction instead, this row comes
	// back "pending" and the mailbox is retried forever.
	assert.Equal(t, entity.MailAccountStatusError, parked.Status,
		"a revoked mailbox has to leave the poll queue and show up as broken")
	require.NotNil(t, parked.LastError)
	assert.Contains(t, *parked.LastError, "reconnect",
		"the dashboard message has to tell the user what to do about it")

	t.Run("the failure is not retried", func(t *testing.T) {
		_, before := h.OAuth.Counts()

		h.Post(t, "/api/mail-accounts/"+stored.ID+"/_verify", user.Token, nil)

		_, after := h.OAuth.Counts()
		assert.Equal(t, before+1, after,
			"one attempt per explicit request is fine; what must not happen is a loop inside one")
	})
}

// Consenting again to a mailbox that is already here has to update it rather
// than leave a duplicate behind, and it must not lose the refresh token in the
// process — Google omits that from every consent after the first.
func TestOAuthReconnect(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	user := h.Register(t, "oauth-reconnect@example.com", "secret123")

	connect := func(t *testing.T) support.Response {
		t.Helper()

		authorize := h.Get(t, "/api/mail-accounts/oauth/yandex/_authorize", user.Token)
		require.Equal(t, http.StatusOK, authorize.Status, authorize.Error(t))

		var started authorizeResponse
		authorize.Decode(t, &started)

		return h.Get(t, callbackPath("yandex", "the-code", started.State), "")
	}

	first := connect(t)
	require.Empty(t, oauthError(t, first.Location()))

	original := accountRow(t, h, "connected@yandex.com")

	// the second consent hands back a new access token and no refresh token,
	// exactly as Google does
	h.OAuth.Configure(func(c *support.StubConfig) {
		c.AccessToken = "second-access-token"
		c.RefreshToken = ""
	})

	second := connect(t)
	require.Empty(t, oauthError(t, second.Location()))

	var total int64
	require.NoError(t, h.DB.Model(&entity.MailAccount{}).Count(&total).Error)
	assert.Equal(t, int64(1), total, "consenting again left a duplicate mailbox polling")

	updated := accountRow(t, h, "connected@yandex.com")
	assert.Equal(t, original.ID, updated.ID, "the same row should have been updated")

	credentials := decryptCredentials(t, h, updated)
	assert.Equal(t, "second-access-token", credentials.AccessToken)
	assert.Equal(t, "stub-refresh-token", credentials.RefreshToken,
		"an absent refresh token means keep the old one; overwriting it kills the mailbox an hour later")
}
