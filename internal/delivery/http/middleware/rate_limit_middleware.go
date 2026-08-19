package middleware

import (
	"strconv"
	"time"

	"mailpulse/internal/gateway/cache"

	"github.com/gofiber/fiber/v2"
)

// NewRateLimit throttles a route by caller IP.
//
// The key is ctx.IP() rather than the X-Forwarded-For header the audit trail
// prefers. A limiter keyed on a header the caller controls is not a limiter at
// all — an attacker varies the header and the counter never fills. Fiber only
// derives ctx.IP() from the proxy header when the connection came from a
// configured trusted proxy, so this is the value that cannot be forged.
//
// scope namespaces the counter so a shared IP hitting login does not consume
// its forgot-password allowance as well.
func NewRateLimit(limiter *cache.RateLimiter, scope string, limit int, window time.Duration) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		allowed, retryAfter := limiter.Allow(ctx.UserContext(), scope, ctx.IP(), limit, window)
		if allowed {
			return ctx.Next()
		}

		ctx.Set(fiber.HeaderRetryAfter, strconv.Itoa(int(retryAfter.Seconds())))
		return fiber.NewError(fiber.StatusTooManyRequests,
			"too many attempts, please wait before trying again")
	}
}
