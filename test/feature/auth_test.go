//go:build feature

package feature

import (
	"net/http"
	"testing"

	"mailpulse/test/support"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegister(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	t.Run("creates an account", func(t *testing.T) {
		response := h.Post(t, "/api/users", "", map[string]string{
			"email": "new@example.com", "name": "New", "password": "secret123",
		})
		require.Equal(t, http.StatusCreated, response.Status)

		var user struct {
			ID    string   `json:"id"`
			Email string   `json:"email"`
			Roles []string `json:"roles"`
		}
		response.Decode(t, &user)

		assert.NotEmpty(t, user.ID)
		assert.Equal(t, "new@example.com", user.Email)
		assert.Equal(t, []string{"user"}, user.Roles, "every account starts as a plain user")
	})

	t.Run("rejects a duplicate address", func(t *testing.T) {
		response := h.Post(t, "/api/users", "", map[string]string{
			"email": "new@example.com", "name": "New", "password": "secret123",
		})
		assert.Equal(t, http.StatusConflict, response.Status)
	})

	t.Run("normalises the address", func(t *testing.T) {
		response := h.Post(t, "/api/users", "", map[string]string{
			"email": "  MiXeD@Example.COM  ", "name": "Mixed", "password": "secret123",
		})
		require.Equal(t, http.StatusCreated, response.Status)

		var user struct {
			Email string `json:"email"`
		}
		response.Decode(t, &user)
		assert.Equal(t, "mixed@example.com", user.Email)
	})

	t.Run("validates the payload", func(t *testing.T) {
		for name, payload := range map[string]map[string]string{
			"no email":       {"name": "X", "password": "secret123"},
			"bad email":      {"email": "not-an-address", "name": "X", "password": "secret123"},
			"short password": {"email": "short@example.com", "name": "X", "password": "12"},
			"no name":        {"email": "noname@example.com", "password": "secret123"},
		} {
			t.Run(name, func(t *testing.T) {
				assert.Equal(t, http.StatusBadRequest, h.Post(t, "/api/users", "", payload).Status)
			})
		}
	})
}

func TestLogin(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	h.Register(t, "login@example.com", "secret123")

	t.Run("returns a usable token", func(t *testing.T) {
		response := h.Post(t, "/api/users/_login", "", map[string]string{
			"email": "login@example.com", "password": "secret123",
		})
		require.Equal(t, http.StatusOK, response.Status)

		var login struct {
			Token     string `json:"token"`
			ExpiresAt int64  `json:"expires_at"`
			User      struct {
				Email string `json:"email"`
			} `json:"user"`
		}
		response.Decode(t, &login)

		assert.NotEmpty(t, login.Token)
		assert.Greater(t, login.ExpiresAt, int64(0))
		assert.Equal(t, "login@example.com", login.User.Email)

		assert.Equal(t, http.StatusOK, h.Get(t, "/api/users/_current", login.Token).Status)
	})

	t.Run("wrong password", func(t *testing.T) {
		response := h.Post(t, "/api/users/_login", "", map[string]string{
			"email": "login@example.com", "password": "wrong",
		})
		assert.Equal(t, http.StatusUnauthorized, response.Status)
	})

	// an unknown address must be indistinguishable from a wrong password, or
	// the endpoint becomes a way to enumerate who has an account
	t.Run("unknown address looks the same as a wrong password", func(t *testing.T) {
		response := h.Post(t, "/api/users/_login", "", map[string]string{
			"email": "nobody@example.com", "password": "secret123",
		})
		assert.Equal(t, http.StatusUnauthorized, response.Status)
	})
}

func TestCurrentUserAndUpdate(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	account := h.Register(t, "profile@example.com", "secret123")

	updated := h.Patch(t, "/api/users/_current", account.Token, map[string]string{
		"name": "Renamed", "timezone": "Europe/Riga",
	})
	require.Equal(t, http.StatusOK, updated.Status)

	current := h.Get(t, "/api/users/_current", account.Token)
	var user struct {
		Name     string `json:"name"`
		Timezone string `json:"timezone"`
	}
	current.Decode(t, &user)

	assert.Equal(t, "Renamed", user.Name)
	assert.Equal(t, "Europe/Riga", user.Timezone)
}

func TestSessions(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	account := h.Register(t, "sessions@example.com", "secret123")

	// a second login is a second device, and must not disturb the first
	second := h.Login(t, "sessions@example.com", "secret123")

	assert.Equal(t, http.StatusOK, h.Get(t, "/api/users/_current", account.Token).Status,
		"logging in elsewhere must not invalidate an existing session")
	assert.Equal(t, http.StatusOK, h.Get(t, "/api/users/_current", second).Status)

	listed := h.Get(t, "/api/users/_sessions", account.Token)
	var sessions []struct {
		ID      string `json:"id"`
		Current bool   `json:"current"`
	}
	listed.Decode(t, &sessions)

	assert.Len(t, sessions, 2)
	current := 0
	for _, session := range sessions {
		if session.Current {
			current++
		}
	}
	assert.Equal(t, 1, current, "exactly one session is the caller's own")
}

func TestLogout(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	account := h.Register(t, "logout@example.com", "secret123")

	require.Equal(t, http.StatusOK, h.Delete(t, "/api/users", account.Token).Status)

	// the cache is evicted on logout, so the token stops working immediately
	// rather than lingering for the ttl
	assert.Equal(t, http.StatusUnauthorized, h.Get(t, "/api/users/_current", account.Token).Status)
}

func TestPasswordReset(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	h.Register(t, "reset@example.com", "secret123")

	// an unknown address still reports success, so the endpoint cannot be used
	// to discover who is registered
	unknown := h.Post(t, "/api/users/_forgot-password", "", map[string]string{"email": "nobody@example.com"})
	assert.Equal(t, http.StatusOK, unknown.Status)

	known := h.Post(t, "/api/users/_forgot-password", "", map[string]string{"email": "reset@example.com"})
	assert.Equal(t, http.StatusOK, known.Status)

	invalid := h.Post(t, "/api/users/_reset-password", "", map[string]string{
		"token": "not-a-real-token", "password": "newsecret123",
	})
	assert.Equal(t, http.StatusBadRequest, invalid.Status)
}
