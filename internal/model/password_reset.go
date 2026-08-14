package model

// PasswordReset is the value behind a reset token in redis.
type PasswordReset struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
}
