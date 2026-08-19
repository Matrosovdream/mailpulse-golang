package middleware

import (
	"strings"

	"mailpulse/internal/model"
	"mailpulse/internal/usecase"

	"github.com/gofiber/fiber/v2"
)

const (
	authLocalsKey  = "auth"
	tokenLocalsKey = "auth_token"
)

// NewAuth resolves the bearer token to an Auth and puts it on the request.
// Both the raw header form and "Bearer <token>" are accepted, because every
// SPA HTTP client defaults to one or the other.
func NewAuth(userUseCase *usecase.UserUseCase) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		token := strings.TrimSpace(ctx.Get("Authorization"))
		token = strings.TrimPrefix(token, "Bearer ")
		token = strings.TrimPrefix(token, "bearer ")

		if token == "" {
			return fiber.ErrUnauthorized
		}

		auth, err := userUseCase.Verify(ctx.UserContext(), &model.VerifyUserRequest{Token: token})
		if err != nil {
			return err
		}

		ctx.Locals(authLocalsKey, auth)
		ctx.Locals(tokenLocalsKey, token)

		// also carried on the context the usecases receive, so audit writes can
		// see who is really acting without 26 call sites passing it by hand
		ctx.SetUserContext(model.ContextWithAuth(ctx.UserContext(), auth))

		return ctx.Next()
	}
}

// GetUser returns the authenticated caller. Only ever called from handlers
// mounted behind NewAuth, so the value is always present.
func GetUser(ctx *fiber.Ctx) *model.Auth {
	auth, ok := ctx.Locals(authLocalsKey).(*model.Auth)
	if !ok {
		return &model.Auth{}
	}
	return auth
}

// GetToken returns the raw bearer token, which logout needs in order to derive
// the hash the cache entry is keyed by.
func GetToken(ctx *fiber.Ctx) string {
	token, _ := ctx.Locals(tokenLocalsKey).(string)
	return token
}
