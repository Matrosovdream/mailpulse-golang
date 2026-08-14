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

// clientIP prefers the proxy header, since the app is meant to run behind one.
func clientIP(ctx *fiber.Ctx) string {
	if forwarded := ctx.Get("X-Forwarded-For"); forwarded != "" {
		return forwarded
	}
	return ctx.IP()
}
