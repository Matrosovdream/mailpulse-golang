package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

// NewCORS allows a browser on one of the given origins to read responses from
// this API.
//
// The origins are an explicit allowlist, never a wildcard. Fiber treats an
// empty AllowOrigins as "*", so callers must not reach this with an empty
// slice — RouteConfig.SetupCORS skips mounting entirely in that case, which is
// what keeps an unconfigured deployment from silently serving every origin.
//
// AllowCredentials stays off because a session here is a bearer token in the
// Authorization header, not a cookie, and nothing needs the browser to attach
// ambient credentials. Turning it on would also make a wildcard origin a
// startup panic, which is a guard worth keeping rather than working around.
func NewCORS(origins []string) fiber.Handler {
	return cors.New(cors.Config{
		AllowOrigins: strings.Join(origins, ","),
		AllowMethods: strings.Join([]string{
			fiber.MethodGet,
			fiber.MethodPost,
			fiber.MethodPut,
			fiber.MethodPatch,
			fiber.MethodDelete,
			fiber.MethodOptions,
		}, ","),
		AllowHeaders: strings.Join([]string{
			fiber.HeaderAuthorization,
			fiber.HeaderContentType,
		}, ","),
		// Retry-After is set by NewRateLimit on a 429. A browser will not let
		// page script read a response header unless it is named here, so
		// without this the SPA can see that it was throttled but not for how
		// long — which is the only part a user can act on.
		ExposeHeaders:    fiber.HeaderRetryAfter,
		AllowCredentials: false,
		// cache the preflight for an hour, so a browsing session pays the extra
		// round trip once per route rather than on every mutation
		MaxAge: 3600,
	})
}
