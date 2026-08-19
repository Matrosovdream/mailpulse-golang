//go:build feature

package feature

import (
	"net/http"
	"testing"

	"mailpulse/test/support"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every authenticated route must reject an anonymous caller. This walks the
// groups rather than one example, because the guard is mounted per group and a
// route added to the wrong group is exactly the mistake worth catching.
func TestAuthenticationIsRequired(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	protected := []struct{ method, path string }{
		{http.MethodGet, "/api/users/_current"},
		{http.MethodGet, "/api/users/_sessions"},
		{http.MethodGet, "/api/mail-accounts"},
		{http.MethodPost, "/api/mail-accounts"},
		{http.MethodGet, "/api/notifiers"},
		{http.MethodGet, "/api/watchers"},
		{http.MethodGet, "/api/matches"},
		{http.MethodGet, "/api/event-runs"},
		{http.MethodGet, "/api/deliveries"},
		{http.MethodGet, "/api/dashboard/summary"},
		{http.MethodGet, "/api/mail-provider-types"},
		{http.MethodGet, "/api/event-types"},
		{http.MethodGet, "/api/notifier-types"},
		{http.MethodGet, "/api/filter-fields"},
		{http.MethodGet, "/api/admin/stats"},
		{http.MethodGet, "/api/admin/users"},
	}

	for _, route := range protected {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			assert.Equal(t, http.StatusUnauthorized,
				h.Do(t, route.method, route.path, "", nil).Status)

			assert.Equal(t, http.StatusUnauthorized,
				h.Do(t, route.method, route.path, "garbage-token", nil).Status)
		})
	}
}

func TestPublicRoutesNeedNoToken(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	assert.Equal(t, http.StatusOK, h.Get(t, "/api/health", "").Status)
}

func TestAdminRoutesRequireSuperadmin(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	plain := h.Register(t, "plain@example.com", "secret123")

	adminRoutes := []string{
		"/api/admin/stats",
		"/api/admin/users",
		"/api/admin/watchers",
		"/api/admin/mail-accounts",
		"/api/admin/notifiers",
		"/api/admin/event-runs",
		"/api/admin/audit-logs",
	}

	t.Run("a plain user is refused", func(t *testing.T) {
		for _, path := range adminRoutes {
			assert.Equal(t, http.StatusForbidden, h.Get(t, path, plain.Token).Status, path)
		}
	})

	t.Run("a superadmin is allowed", func(t *testing.T) {
		admin := h.Register(t, "admin@example.com", "secret123")
		h.MakeSuperadmin(t, admin.ID)

		// the role rides along in the cached auth, so a fresh token is needed
		token := h.Login(t, "admin@example.com", "secret123")

		for _, path := range adminRoutes {
			assert.Equal(t, http.StatusOK, h.Get(t, path, token).Status, path)
		}
	})
}

// Tenant isolation is the property most worth a test: a wrong owner must be
// indistinguishable from a missing row, so an id cannot be probed for existence.
func TestUsersCannotReachEachOthersData(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	owner := h.Register(t, "owner@example.com", "secret123")
	stranger := h.Register(t, "stranger@example.com", "secret123")

	created := h.Post(t, "/api/mail-accounts", owner.Token, map[string]any{
		"provider": "imap", "email_address": "owned@corp.com",
		"auth_mode": "password", "password": "pw",
		"settings": map[string]any{"host": "mail.invalid", "port": 993},
	})
	require.Equal(t, http.StatusCreated, created.Status)

	var account struct {
		ID string `json:"id"`
	}
	created.Decode(t, &account)

	watcherResponse := h.Post(t, "/api/watchers", owner.Token, map[string]any{
		"mail_account_id": account.ID, "name": "Private",
	})
	require.Equal(t, http.StatusCreated, watcherResponse.Status)

	var watcher struct {
		ID string `json:"id"`
	}
	watcherResponse.Decode(t, &watcher)

	t.Run("cannot read", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, h.Get(t, "/api/mail-accounts/"+account.ID, stranger.Token).Status)
		assert.Equal(t, http.StatusNotFound, h.Get(t, "/api/watchers/"+watcher.ID, stranger.Token).Status)
	})

	t.Run("cannot modify", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound,
			h.Patch(t, "/api/watchers/"+watcher.ID, stranger.Token, map[string]string{"name": "Stolen"}).Status)
		assert.Equal(t, http.StatusNotFound,
			h.Delete(t, "/api/watchers/"+watcher.ID, stranger.Token).Status)
	})

	t.Run("lists are scoped to the caller", func(t *testing.T) {
		listed := h.Get(t, "/api/watchers?status=all", stranger.Token)
		var watchers []struct {
			ID string `json:"id"`
		}
		listed.Decode(t, &watchers)
		assert.Empty(t, watchers)
	})

	t.Run("the owner is unaffected", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, h.Get(t, "/api/watchers/"+watcher.ID, owner.Token).Status)
	})
}
