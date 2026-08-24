package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func configured() Config {
	return Config{ClientID: "client-id", ClientSecret: "client-secret"}
}

// parse pulls the consent URL apart so assertions read as claims about
// parameters rather than substring searches.
func parse(t *testing.T, raw string) url.Values {
	t.Helper()
	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	return parsed.Query()
}

func TestConfig_Configured(t *testing.T) {
	assert.False(t, Config{}.Configured())
	assert.False(t, Config{ClientID: "id"}.Configured(), "half a credential is not a credential")
	assert.False(t, Config{ClientSecret: "secret"}.Configured())
	assert.True(t, configured().Configured())
}

// An unconfigured provider must produce no client at all. That is what lets
// _authorize answer "not configured yet" instead of sending a user to a consent
// screen that would turn them away.
func TestNewProviders_ReturnNilWhenUnconfigured(t *testing.T) {
	assert.Nil(t, NewGoogle(Config{}, "http://localhost:3000"))
	assert.Nil(t, NewMicrosoft(Config{}, "http://localhost:3000"))
	assert.Nil(t, NewYandex(Config{}, "http://localhost:3000"))
	assert.Nil(t, NewStub("yandex", ""))
}

func TestCallbackURL(t *testing.T) {
	assert.Equal(t, "https://api.example.com/api/mail-accounts/oauth/gmail/_callback",
		CallbackURL("https://api.example.com", "gmail"))

	assert.Equal(t, "https://api.example.com/api/mail-accounts/oauth/gmail/_callback",
		CallbackURL("https://api.example.com/", "gmail"),
		"a trailing slash on the base must not double up: the redirect_uri is compared byte for byte")
}

func TestGoogle_AuthorizeURL(t *testing.T) {
	query := parse(t, NewGoogle(configured(), "https://api.example.com").AuthorizeURL("the-state"))

	assert.Equal(t, "the-state", query.Get("state"))
	assert.Equal(t, "client-id", query.Get("client_id"))
	assert.Equal(t, "https://api.example.com/api/mail-accounts/oauth/gmail/_callback",
		query.Get("redirect_uri"))

	// both of these are load-bearing: without access_type=offline Google issues
	// no refresh token, and without prompt=consent it stops issuing one on
	// every consent after the first
	assert.Equal(t, "offline", query.Get("access_type"))
	assert.Equal(t, "consent", query.Get("prompt"))

	// mail.google.com, not gmail.readonly — the narrow one is refused by IMAP
	assert.Contains(t, query.Get("scope"), "https://mail.google.com/")
	assert.NotContains(t, query.Get("scope"), "gmail.readonly")
	assert.Contains(t, query.Get("scope"), "email")
}

func TestMicrosoft_AuthorizeURL(t *testing.T) {
	client := NewMicrosoft(configured(), "https://api.example.com")
	raw := client.AuthorizeURL("the-state")

	assert.Contains(t, raw, "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
		"an unset tenant has to fall back to common or personal accounts are refused")

	query := parse(t, raw)
	assert.Equal(t, "https://api.example.com/api/mail-accounts/oauth/outlook/_callback",
		query.Get("redirect_uri"))
	assert.Contains(t, query.Get("scope"), "https://outlook.office.com/IMAP.AccessAsUser.All")
	assert.Contains(t, query.Get("scope"), "offline_access", "this is how v2.0 asks for a refresh token")
	assert.Contains(t, query.Get("scope"), "openid", "the id_token is the only way we learn the address")
}

func TestMicrosoft_HonoursTheTenant(t *testing.T) {
	config := configured()
	config.Tenant = "contoso.onmicrosoft.com"

	raw := NewMicrosoft(config, "https://api.example.com").AuthorizeURL("s")

	assert.Contains(t, raw, "/contoso.onmicrosoft.com/oauth2/v2.0/authorize")
}

func TestYandex_AuthorizeURL(t *testing.T) {
	query := parse(t, NewYandex(configured(), "https://api.example.com").AuthorizeURL("the-state"))

	assert.Equal(t, "https://api.example.com/api/mail-accounts/oauth/yandex/_callback",
		query.Get("redirect_uri"))
	assert.Contains(t, query.Get("scope"), "mail:imap_ro", "this application only ever reads")
	assert.Contains(t, query.Get("scope"), "login:email")
}

func TestRegistry(t *testing.T) {
	registry := NewRegistry()

	// nil is what an unconfigured provider constructor returns, and Register
	// takes it silently so Bootstrap can pass all three unconditionally
	registry.Register(NewGoogle(Config{}, "http://localhost"), NewYandex(configured(), "http://localhost"))

	assert.False(t, registry.Has("gmail"))
	assert.True(t, registry.Has("yandex"))
	assert.Equal(t, []string{"yandex"}, registry.Slugs())

	_, ok := registry.Get("outlook")
	assert.False(t, ok)
}

func TestIdentity_FromIDToken(t *testing.T) {
	claims := func(payload map[string]any) string {
		encoded, err := json.Marshal(payload)
		require.NoError(t, err)
		return "header." + base64.RawURLEncoding.EncodeToString(encoded) + ".signature"
	}

	t.Run("reads the email claim", func(t *testing.T) {
		identity, err := microsoftIdentify(context.Background(), Tokens{
			AccessToken: "token",
			IDToken:     claims(map[string]any{"email": "user@contoso.com", "oid": "object-id"}),
		})

		require.NoError(t, err)
		assert.Equal(t, "user@contoso.com", identity.Email)
		assert.Equal(t, "object-id", identity.AccountID)
	})

	// work and school accounts frequently carry no email claim at all, and the
	// UPN is the address IMAP wants
	t.Run("falls back to preferred_username", func(t *testing.T) {
		identity, err := microsoftIdentify(context.Background(), Tokens{
			AccessToken: "token",
			IDToken:     claims(map[string]any{"preferred_username": "user@contoso.com", "sub": "s"}),
		})

		require.NoError(t, err)
		assert.Equal(t, "user@contoso.com", identity.Email)
	})

	t.Run("says so when there is no id_token", func(t *testing.T) {
		_, err := microsoftIdentify(context.Background(), Tokens{AccessToken: "token"})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "openid")
	})

	t.Run("rejects something that is not a JWT", func(t *testing.T) {
		_, err := microsoftIdentify(context.Background(), Tokens{AccessToken: "t", IDToken: "not-a-jwt"})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a JWT")
	})
}

// Yandex authenticates with the OAuth scheme rather than Bearer, and answers
// 401 to the wrong one — a mistake that would only show up against the live
// service.
func TestYandex_IdentifyUsesTheOAuthScheme(t *testing.T) {
	var seen string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id": "1234", "default_email": "someone@yandex.ru",
		})
	}))
	defer server.Close()

	var info struct {
		ID           string `json:"id"`
		DefaultEmail string `json:"default_email"`
	}
	require.NoError(t, getJSON(context.Background(), server.URL, "OAuth the-token", &info))

	assert.Equal(t, "OAuth the-token", seen)
	assert.Equal(t, "someone@yandex.ru", info.DefaultEmail)
}

// Telling a revoked grant apart from a bad gateway is the whole reason the
// resolver can park an account instead of retrying it forever.
func TestRefresh_TranslatesInvalidGrant(t *testing.T) {
	tokenServer := func(t *testing.T, status int, body string) Client {
		t.Helper()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(server.Close)
		return NewStub("yandex", server.URL)
	}

	t.Run("invalid_grant is terminal", func(t *testing.T) {
		client := tokenServer(t, http.StatusBadRequest,
			`{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`)

		_, err := client.Refresh(context.Background(), "stale-refresh-token")

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrRevoked)
		assert.Contains(t, err.Error(), "revoked")
	})

	t.Run("anything else is transient", func(t *testing.T) {
		client := tokenServer(t, http.StatusInternalServerError, `{"error":"server_error"}`)

		_, err := client.Refresh(context.Background(), "good-refresh-token")

		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrRevoked,
			"a five hundred is the provider having a bad minute, not the user withdrawing consent")
	})

	t.Run("a missing refresh token is terminal too", func(t *testing.T) {
		client := tokenServer(t, http.StatusOK, `{}`)

		_, err := client.Refresh(context.Background(), "")

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrRevoked, "there is nothing to retry with")
	})
}

// Google returns no refresh token when refreshing, only on first consent.
// Carrying the old one forward here is what stops every caller having to know.
func TestRefresh_KeepsTheExistingRefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-access","token_type":"Bearer","expires_in":3600}`))
	}))
	defer server.Close()

	tokens, err := NewStub("yandex", server.URL).Refresh(context.Background(), "the-original-refresh-token")

	require.NoError(t, err)
	assert.Equal(t, "new-access", tokens.AccessToken)
	assert.Equal(t, "the-original-refresh-token", tokens.RefreshToken)
	assert.Greater(t, tokens.ExpiresAt, int64(0), "expiry is epoch millis, like every other timestamp")
}

func TestGetJSON_FailsOnANonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	err := getJSON(context.Background(), server.URL, "Bearer t", &struct{}{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}
