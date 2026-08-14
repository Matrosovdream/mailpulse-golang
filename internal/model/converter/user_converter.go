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

func UserToAuth(user *entity.User, sessionID string) *model.Auth {
	return &model.Auth{
		ID:        user.ID,
		Email:     user.Email,
		Roles:     user.RoleSlugs(),
		SessionID: sessionID,
	}
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
