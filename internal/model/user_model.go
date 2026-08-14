package model

type UserResponse struct {
	ID              string   `json:"id,omitempty"`
	Email           string   `json:"email,omitempty"`
	Name            string   `json:"name,omitempty"`
	Status          string   `json:"status,omitempty"`
	Timezone        string   `json:"timezone,omitempty"`
	Roles           []string `json:"roles"`
	EmailVerifiedAt *int64   `json:"email_verified_at,omitempty"`
	CreatedAt       int64    `json:"created_at,omitempty"`
	UpdatedAt       int64    `json:"updated_at,omitempty"`
}

type LoginResponse struct {
	Token     string        `json:"token"`
	ExpiresAt int64         `json:"expires_at"`
	User      *UserResponse `json:"user"`
}

type SessionResponse struct {
	ID         string  `json:"id"`
	UserAgent  *string `json:"user_agent,omitempty"`
	IP         *string `json:"ip,omitempty"`
	Current    bool    `json:"current"`
	ExpiresAt  int64   `json:"expires_at"`
	LastUsedAt int64   `json:"last_used_at"`
	CreatedAt  int64   `json:"created_at"`
}

type RegisterUserRequest struct {
	Email    string `json:"email" validate:"required,email,max=320"`
	Name     string `json:"name" validate:"required,max=100"`
	Password string `json:"password" validate:"required,min=6,max=100"`
	Timezone string `json:"timezone" validate:"max=64"`
}

type LoginUserRequest struct {
	Email     string `json:"email" validate:"required,email,max=320"`
	Password  string `json:"password" validate:"required,max=100"`
	UserAgent string `json:"-"`
	IP        string `json:"-"`
}

type VerifyUserRequest struct {
	Token string `validate:"required,max=200"`
}

type UpdateUserRequest struct {
	ID       string `json:"-" validate:"required"`
	Name     string `json:"name,omitempty" validate:"max=100"`
	Password string `json:"password,omitempty" validate:"omitempty,min=6,max=100"`
	Timezone string `json:"timezone,omitempty" validate:"max=64"`
}

type GetUserRequest struct {
	ID string `json:"-" validate:"required"`
}

type LogoutUserRequest struct {
	ID        string `json:"-" validate:"required"`
	SessionID string `json:"-" validate:"required"`
	Token     string `json:"-"`
}

type ListSessionRequest struct {
	UserID           string `json:"-" validate:"required"`
	CurrentSessionID string `json:"-"`
}

type RevokeSessionRequest struct {
	UserID    string `json:"-" validate:"required"`
	SessionID string `json:"-" validate:"required"`
}

type ForgotPasswordRequest struct {
	Email string `json:"email" validate:"required,email,max=320"`
}

type ResetPasswordRequest struct {
	Token    string `json:"token" validate:"required,max=200"`
	Password string `json:"password" validate:"required,min=6,max=100"`
}
