package model

import "mailpulse/internal/entity"

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
