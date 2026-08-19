package converter

import (
	"mailpulse/internal/entity"
	"mailpulse/internal/model"
)

func UserToResponse(user *entity.User) *model.UserResponse {
	return &model.UserResponse{
		ID:              user.ID,
		Email:           user.Email,
		Name:            user.Name,
		Status:          user.Status,
		Timezone:        user.Timezone,
		Roles:           user.RoleSlugs(),
		EmailVerifiedAt: user.EmailVerifiedAt,
		CreatedAt:       user.CreatedAt,
		UpdatedAt:       user.UpdatedAt,
	}
}

// UserToAuth builds the value the auth middleware puts on the request. On an
// impersonated session the identity stays the target user — ownership checks
// must keep scoping to their data — and the admin is carried alongside so the
// audit trail can name the human actually responsible.
func UserToAuth(user *entity.User, sessionID string, impersonatedBy *string) *model.Auth {
	auth := &model.Auth{
		ID:        user.ID,
		Email:     user.Email,
		Roles:     user.RoleSlugs(),
		SessionID: sessionID,
	}

	if impersonatedBy != nil && *impersonatedBy != "" {
		auth.Impersonated = true
		auth.ImpersonatedBy = *impersonatedBy
	}

	return auth
}

func SessionToResponse(session *entity.UserSession, currentID string) *model.SessionResponse {
	return &model.SessionResponse{
		ID:         session.ID,
		UserAgent:  session.UserAgent,
		IP:         session.IP,
		Current:    session.ID == currentID,
		ExpiresAt:  session.ExpiresAt,
		LastUsedAt: session.LastUsedAt,
		CreatedAt:  session.CreatedAt,
	}
}

func SessionsToResponses(sessions []entity.UserSession, currentID string) []model.SessionResponse {
	responses := make([]model.SessionResponse, 0, len(sessions))
	for i := range sessions {
		responses = append(responses, *SessionToResponse(&sessions[i], currentID))
	}
	return responses
}

func UserToEvent(user *entity.User) *model.UserEvent {
	return &model.UserEvent{
		ID:        user.ID,
		Name:      user.Name,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
	}
}
