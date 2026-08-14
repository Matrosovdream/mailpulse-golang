package model

import "encoding/json"

type NotifierResponse struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Name       string          `json:"name"`
	Config     json.RawMessage `json:"config"`
	Status     string          `json:"status"`
	VerifiedAt *int64          `json:"verified_at,omitempty"`
	LastError  *string         `json:"last_error,omitempty"`
	IsDefault  bool            `json:"is_default"`
	CreatedAt  int64           `json:"created_at"`
	UpdatedAt  int64           `json:"updated_at"`
}

type CreateNotifierRequest struct {
	UserID    string          `json:"-" validate:"required"`
	Type      string          `json:"type" validate:"required,max=30"`
	Name      string          `json:"name" validate:"required,max=100"`
	Config    json.RawMessage `json:"config"`
	Secrets   json.RawMessage `json:"secrets,omitempty"`
	IsDefault bool            `json:"is_default"`
}

type UpdateNotifierRequest struct {
	UserID    string          `json:"-" validate:"required"`
	ID        string          `json:"-" validate:"required"`
	Name      string          `json:"name" validate:"max=100"`
	Config    json.RawMessage `json:"config,omitempty"`
	Secrets   json.RawMessage `json:"secrets,omitempty"`
	IsDefault *bool           `json:"is_default,omitempty"`
	Status    string          `json:"status" validate:"omitempty,oneof=verified disabled"`
}

type GetNotifierRequest struct {
	UserID string `json:"-" validate:"required"`
	ID     string `json:"-" validate:"required"`
}

type ListNotifierRequest struct {
	PageRequest
	UserID string `json:"-"`
	Type   string `json:"-" validate:"max=30"`
	Status string `json:"-" validate:"omitempty,oneof=pending verified error disabled"`
}

type VerifyNotifierRequest struct {
	UserID string `json:"-" validate:"required"`
	ID     string `json:"-" validate:"required"`
	Code   string `json:"code" validate:"max=20"`
}

type VerifyNotifierResponse struct {
	Status           string  `json:"status"`
	VerificationCode *string `json:"verification_code,omitempty"`
	VerifiedAt       *int64  `json:"verified_at,omitempty"`
	Message          string  `json:"message"`
}

type TestNotifierResponse struct {
	Delivered         bool    `json:"delivered"`
	ProviderMessageID *string `json:"provider_message_id,omitempty"`
	Error             *string `json:"error,omitempty"`
}

// TelegramWebhookRequest is the slice of the Bot API update payload we act on:
// binding a chat to a pending notifier, and acknowledging a run.
type TelegramWebhookRequest struct {
	Message *struct {
		Text string `json:"text"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
	CallbackQuery *struct {
		Data string `json:"data"`
		From struct {
			ID int64 `json:"id"`
		} `json:"from"`
	} `json:"callback_query"`
}
