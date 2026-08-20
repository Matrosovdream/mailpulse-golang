package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// corsApp mounts the middleware the way SetupCORS does — before the route — so
// these tests exercise the ordering as well as the headers.
func corsApp(origins []string) *fiber.App {
	app := fiber.New()
	app.Use(NewCORS(origins))
	app.Get("/api/thing", func(ctx *fiber.Ctx) error {
		return ctx.JSON(fiber.Map{"data": true})
	})
	return app
}

func preflight(t *testing.T, app *fiber.App, origin string) *http.Response {
	t.Helper()

	request := httptest.NewRequest(fiber.MethodOptions, "/api/thing", nil)
	request.Header.Set(fiber.HeaderOrigin, origin)
	request.Header.Set(fiber.HeaderAccessControlRequestMethod, fiber.MethodGet)
	request.Header.Set(fiber.HeaderAccessControlRequestHeaders, "authorization")

	response, err := app.Test(request)
	require.NoError(t, err)
	return response
}

func TestCORSAllowsAConfiguredOrigin(t *testing.T) {
	app := corsApp([]string{"http://localhost:5173"})

	response := preflight(t, app, "http://localhost:5173")

	assert.Equal(t, http.StatusNoContent, response.StatusCode,
		"a preflight should be answered by the middleware, not passed down the chain")
	assert.Equal(t, "http://localhost:5173",
		response.Header.Get(fiber.HeaderAccessControlAllowOrigin))
	assert.Contains(t, response.Header.Get(fiber.HeaderAccessControlAllowHeaders), "Authorization",
		"the browser must be told it may send the bearer token")
	assert.Contains(t, response.Header.Get(fiber.HeaderAccessControlAllowMethods), fiber.MethodPatch)
}

// The allowlist has to actually exclude. This is the test that fails if someone
// "fixes" a CORS problem by widening the origin to a wildcard.
func TestCORSRejectsAnUnknownOrigin(t *testing.T) {
	app := corsApp([]string{"http://localhost:5173"})

	response := preflight(t, app, "https://evil.example.com")

	assert.Empty(t, response.Header.Get(fiber.HeaderAccessControlAllowOrigin),
		"an origin that is not on the list must not be echoed back as allowed")
}

// A simple GET carries the allow-origin header too; without it the browser
// makes the request but discards the response.
func TestCORSSetsHeadersOnASimpleRequest(t *testing.T) {
	app := corsApp([]string{"http://localhost:5173"})

	request := httptest.NewRequest(fiber.MethodGet, "/api/thing", nil)
	request.Header.Set(fiber.HeaderOrigin, "http://localhost:5173")

	response, err := app.Test(request)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, "http://localhost:5173",
		response.Header.Get(fiber.HeaderAccessControlAllowOrigin))
}

// Retry-After is set by NewRateLimit on a 429, and page script cannot read a
// response header unless it is exposed. Without this the SPA can tell that it
// was throttled but not for how long, which is the only actionable part.
func TestCORSExposesRetryAfter(t *testing.T) {
	app := corsApp([]string{"http://localhost:5173"})

	request := httptest.NewRequest(fiber.MethodGet, "/api/thing", nil)
	request.Header.Set(fiber.HeaderOrigin, "http://localhost:5173")

	response, err := app.Test(request)
	require.NoError(t, err)

	assert.Contains(t, response.Header.Get(fiber.HeaderAccessControlExposeHeaders),
		fiber.HeaderRetryAfter)
}

// Credentials mode stays off: auth is a bearer token, not a cookie. It matters
// because fiber panics at startup on AllowCredentials with a wildcard origin,
// and that guard is only meaningful while this stays false.
func TestCORSDoesNotAllowCredentials(t *testing.T) {
	app := corsApp([]string{"http://localhost:5173"})

	response := preflight(t, app, "http://localhost:5173")

	assert.Empty(t, response.Header.Get(fiber.HeaderAccessControlAllowCredentials))
}

// Several origins are the normal case: a dev server and a deployed front end.
func TestCORSAllowsEachConfiguredOrigin(t *testing.T) {
	origins := []string{"http://localhost:5173", "https://app.example.com"}
	app := corsApp(origins)

	for _, origin := range origins {
		response := preflight(t, app, origin)
		assert.Equal(t, origin, response.Header.Get(fiber.HeaderAccessControlAllowOrigin),
			"every configured origin should be allowed")
	}
}
