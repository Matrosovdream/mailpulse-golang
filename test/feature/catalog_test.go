//go:build feature

package feature

import (
	"net/http"
	"testing"

	"mailpulse/test/support"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The catalogs are what a SPA renders its forms from, so their shape is a
// contract: a missing config_schema means a form that cannot be drawn.
func TestMailProviderCatalog(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	account := h.Register(t, "catalog@example.com", "secret123")

	response := h.Get(t, "/api/mail-provider-types", account.Token)
	require.Equal(t, http.StatusOK, response.Status)

	var providers []struct {
		Slug         string         `json:"slug"`
		Label        string         `json:"label"`
		Kind         string         `json:"kind"`
		AuthModes    []string       `json:"auth_modes"`
		Available    bool           `json:"available"`
		Defaults     map[string]any `json:"defaults"`
		ConfigSchema struct {
			Fields []struct {
				Name     string `json:"name"`
				Type     string `json:"type"`
				Required bool   `json:"required"`
			} `json:"fields"`
		} `json:"config_schema"`
	}
	response.Decode(t, &providers)

	require.NotEmpty(t, providers)

	byslug := map[string]int{}
	for i, provider := range providers {
		byslug[provider.Slug] = i

		assert.NotEmpty(t, provider.Label, provider.Slug)
		assert.NotEmpty(t, provider.Kind, provider.Slug)
		assert.NotEmpty(t, provider.AuthModes, provider.Slug)

		if provider.Available {
			assert.NotEmpty(t, provider.ConfigSchema.Fields,
				"%s is offered but has no form to render", provider.Slug)
		}
	}

	t.Run("disabled providers are not offered", func(t *testing.T) {
		_, hasGmail := byslug["gmail"]
		assert.False(t, hasGmail, "gmail is seeded disabled until OAuth exists")
	})

	t.Run("yandex carries its preset", func(t *testing.T) {
		index, ok := byslug["yandex"]
		require.True(t, ok)

		yandex := providers[index]
		assert.Equal(t, "imap", yandex.Kind, "yandex is served by the shared IMAP client")
		assert.Equal(t, "imap.yandex.com", yandex.Defaults["host"])
		assert.Equal(t, float64(993), yandex.Defaults["port"])
		assert.Contains(t, yandex.AuthModes, "app_password")
	})
}

func TestEventAndNotifierCatalogs(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	account := h.Register(t, "types@example.com", "secret123")

	t.Run("event types", func(t *testing.T) {
		response := h.Get(t, "/api/event-types", account.Token)
		require.Equal(t, http.StatusOK, response.Status)

		var types []struct {
			Type          string `json:"type"`
			UsesNotifiers bool   `json:"uses_notifiers"`
		}
		response.Decode(t, &types)

		seen := map[string]bool{}
		for _, item := range types {
			seen[item.Type] = item.UsesNotifiers
		}

		assert.Contains(t, seen, "notify")
		assert.True(t, seen["notify"], "notify is the only handler that fans out to notifiers")
		assert.Contains(t, seen, "webhook")
		assert.False(t, seen["webhook"])
	})

	t.Run("notifier types", func(t *testing.T) {
		response := h.Get(t, "/api/notifier-types", account.Token)
		require.Equal(t, http.StatusOK, response.Status)

		var types []struct {
			Type                 string `json:"type"`
			RequiresVerification bool   `json:"requires_verification"`
		}
		response.Decode(t, &types)

		seen := map[string]bool{}
		for _, item := range types {
			seen[item.Type] = item.RequiresVerification
		}

		assert.Contains(t, seen, "telegram")
		assert.True(t, seen["telegram"], "a chat must prove ownership before we send to it")
		assert.Contains(t, seen, "webhook")
		assert.False(t, seen["webhook"], "a url the user typed needs no round trip")
	})

	t.Run("filter fields pair with legal operators", func(t *testing.T) {
		response := h.Get(t, "/api/filter-fields", account.Token)
		require.Equal(t, http.StatusOK, response.Status)

		var fields []struct {
			Field     string   `json:"field"`
			Operators []string `json:"operators"`
		}
		response.Decode(t, &fields)

		require.NotEmpty(t, fields)
		for _, field := range fields {
			assert.NotEmpty(t, field.Operators, "%s offers no operators", field.Field)
		}
	})
}

func TestHealth(t *testing.T) {
	h := support.New(t)

	response := h.Get(t, "/api/health", "")
	require.Equal(t, http.StatusOK, response.Status)

	var health struct {
		Database string `json:"database"`
		Redis    string `json:"redis"`
	}
	response.Decode(t, &health)

	assert.Equal(t, "ok", health.Database)
	assert.Equal(t, "ok", health.Redis)
}
