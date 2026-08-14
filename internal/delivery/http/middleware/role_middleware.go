package middleware

import (
	"github.com/gofiber/fiber/v2"
)

// NewRequireRole gates a route group on the caller holding one of the given
// role slugs. Roles come off the cached Auth, so this costs no query.
//
// It must be mounted after NewAuth; on its own it denies everything.
func NewRequireRole(slugs ...string) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		auth := GetUser(ctx)

		for _, slug := range slugs {
			if auth.HasRole(slug) {
				return ctx.Next()
			}
		}

		return fiber.NewError(fiber.StatusForbidden, "this area needs additional permissions")
	}
}
