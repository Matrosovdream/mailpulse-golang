package config

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/spf13/viper"
)

func NewFiber(config *viper.Viper) *fiber.App {
	// With a trusted proxy list configured, ctx.IP() reports the address from
	// X-Forwarded-For, but only when the connection itself came from one of
	// these. Without the check any caller could set the header and pick their
	// own identity, which is worth nothing for logging and actively harmful for
	// the rate limiter keyed on it.
	trustedProxies := splitAndTrim(config.GetString("web.trusted_proxies"))

	var app = fiber.New(fiber.Config{
		AppName:                 config.GetString("app.name"),
		ErrorHandler:            NewErrorHandler(),
		Prefork:                 config.GetBool("web.prefork"),
		ProxyHeader:             fiber.HeaderXForwardedFor,
		EnableTrustedProxyCheck: true,
		TrustedProxies:          trustedProxies,
	})

	return app
}

func splitAndTrim(csv string) []string {
	if strings.TrimSpace(csv) == "" {
		return nil
	}

	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func NewErrorHandler() fiber.ErrorHandler {
	return func(ctx *fiber.Ctx, err error) error {
		code := fiber.StatusInternalServerError
		if e, ok := err.(*fiber.Error); ok {
			code = e.Code
		}

		return ctx.Status(code).JSON(fiber.Map{
			"errors": err.Error(),
		})
	}
}
