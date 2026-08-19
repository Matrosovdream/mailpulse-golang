package model

import (
	"context"

	"mailpulse/internal/entity"
)

// Auth is what the auth middleware puts on the request and what the Redis
// cache stores against a token hash. Roles ride along so a role check never
// costs a query — the trade is that changing a user's roles must evict their
// cached sessions, which UserUseCase does.
type Auth struct {
	ID             string   `json:"id"`
	Email          string   `json:"email"`
	Roles          []string `json:"roles"`
	SessionID      string   `json:"session_id"`
	Impersonated   bool     `json:"impersonated,omitempty"`
	ImpersonatedBy string   `json:"impersonated_by,omitempty"`
}

func (a *Auth) HasRole(slug string) bool {
	for _, role := range a.Roles {
		if role == slug {
			return true
		}
	}
	return false
}

func (a *Auth) IsSuperadmin() bool {
	return a.HasRole(entity.RoleSuperadmin)
}

// authContextKey is an unexported struct type so nothing outside this package
// can collide with the key or overwrite the value.
type authContextKey struct{}

// ContextWithAuth carries the caller down into the usecases. The auth
// middleware puts it on the request context so audit writes can tell who is
// really acting without every call site having to pass it along by hand.
func ContextWithAuth(ctx context.Context, auth *Auth) context.Context {
	return context.WithValue(ctx, authContextKey{}, auth)
}

// AuthFromContext returns nil on unauthenticated paths — worker loops, public
// routes, and tests that call a usecase directly.
func AuthFromContext(ctx context.Context) *Auth {
	auth, _ := ctx.Value(authContextKey{}).(*Auth)
	return auth
}
