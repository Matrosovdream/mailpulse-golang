//go:build feature

// CORS through the real route table. The middleware's own behaviour is covered
// by a unit test next to it; what can only be checked here is the ordering —
// that the preflight is answered before the /api group's auth middleware gets
// to reject it.
package feature

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mailpulse/test/support"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allowedOrigin returns the first configured origin, or skips: with none set
// the middleware is deliberately not mounted, so there is nothing to assert
// and a pass would mean nothing.
func allowedOrigin(t *testing.T, h *support.Harness) string {
	t.Helper()

	origins := h.Config.GetString("web.cors_origins")
	if origins == "" {
		t.Skip("WEB_CORS_ORIGINS is not set, so no CORS middleware is mounted")
	}

	first := strings.TrimSpace(strings.Split(origins, ",")[0])
	if first == "" {
		t.Skip("WEB_CORS_ORIGINS is set but empty after trimming")
	}
	return first
}

// The regression this whole change exists for. A browser sends its preflight
// without the Authorization header; before CORS was mounted ahead of the /api
// group, the auth middleware answered it 401 and the real request was never
// sent, so the API worked from curl and not from a browser.
func TestPreflightOnAProtectedRouteIsNotRejectedByAuth(t *testing.T) {
	h := support.New(t)
	origin := allowedOrigin(t, h)

	request := httptest.NewRequest(fiber.MethodOptions, "/api/watchers", nil)
	request.Header.Set(fiber.HeaderOrigin, origin)
	request.Header.Set(fiber.HeaderAccessControlRequestMethod, fiber.MethodGet)
	request.Header.Set(fiber.HeaderAccessControlRequestHeaders, "authorization")

	response, err := h.App.Test(request)
	require.NoError(t, err)

	assert.Equal(t, http.StatusNoContent, response.StatusCode,
		"the preflight must be answered by CORS, not rejected by the auth middleware")
	assert.Equal(t, origin, response.Header.Get(fiber.HeaderAccessControlAllowOrigin))
	assert.Contains(t, response.Header.Get(fiber.HeaderAccessControlAllowHeaders), "Authorization")
}

// An authenticated GET has to carry the allow-origin header, or the browser
// makes the call and then throws the answer away.
func TestAuthenticatedRequestCarriesAllowOrigin(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	origin := allowedOrigin(t, h)

	account := h.Register(t, "cors-user@example.com", "secret123")

	request := httptest.NewRequest(fiber.MethodGet, "/api/watchers", nil)
	request.Header.Set(fiber.HeaderOrigin, origin)
	request.Header.Set(fiber.HeaderAuthorization, "Bearer "+account.Token)

	response, err := h.App.Test(request)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, origin, response.Header.Get(fiber.HeaderAccessControlAllowOrigin))
	assert.Contains(t, response.Header.Get(fiber.HeaderAccessControlExposeHeaders),
		fiber.HeaderRetryAfter, "the SPA needs to read Retry-After to show a wait")
}

// The public routes are reachable cross-origin too — the SPA has to be able to
// log in before it has a token.
func TestPublicRouteIsReachableCrossOrigin(t *testing.T) {
	h := support.New(t)
	origin := allowedOrigin(t, h)

	request := httptest.NewRequest(fiber.MethodOptions, "/api/users/_login", nil)
	request.Header.Set(fiber.HeaderOrigin, origin)
	request.Header.Set(fiber.HeaderAccessControlRequestMethod, fiber.MethodPost)

	response, err := h.App.Test(request)
	require.NoError(t, err)

	assert.Equal(t, http.StatusNoContent, response.StatusCode)
	assert.Equal(t, origin, response.Header.Get(fiber.HeaderAccessControlAllowOrigin))
}

// An origin that was never configured must not be echoed back, whatever it is.
func TestUnknownOriginIsNotAllowed(t *testing.T) {
	h := support.New(t)
	allowedOrigin(t, h) // skip when CORS is off, same as the rest

	request := httptest.NewRequest(fiber.MethodOptions, "/api/watchers", nil)
	request.Header.Set(fiber.HeaderOrigin, "https://evil.example.com")
	request.Header.Set(fiber.HeaderAccessControlRequestMethod, fiber.MethodGet)

	response, err := h.App.Test(request)
	require.NoError(t, err)

	assert.Empty(t, response.Header.Get(fiber.HeaderAccessControlAllowOrigin),
		"an unconfigured origin must never be echoed back as allowed")
}
