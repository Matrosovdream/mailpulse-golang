package http

import (
	"strconv"

	"mailpulse/internal/model"

	"github.com/gofiber/fiber/v2"
)

// paging reads ?page= and ?size= with the same defaults everywhere.
func paging(ctx *fiber.Ctx) model.PageRequest {
	page := model.PageRequest{
		Page: ctx.QueryInt("page", 1),
		Size: ctx.QueryInt("size", 20),
	}
	page.Normalize()
	return page
}

// queryMillis reads a millisecond timestamp filter, tolerating an empty value.
func queryMillis(ctx *fiber.Ctx, key string) int64 {
	raw := ctx.Query(key)
	if raw == "" {
		return 0
	}

	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return value
}

// clientIP is the address recorded on audit rows and sessions.
//
// It defers to Fiber, which reads X-Forwarded-For only when the connection came
// from an address in WEB_TRUSTED_PROXIES and otherwise reports the peer. Taking
// the header at face value, as this used to, meant any caller could write an
// arbitrary IP into the audit trail simply by setting it — which is worse than
// recording nothing, because the value reads as evidence.
func clientIP(ctx *fiber.Ctx) string {
	return ctx.IP()
}
