package model

import "encoding/json"

type MailAccountResponse struct {
	ID                  string          `json:"id"`
	Provider            string          `json:"provider"`
	ProviderLabel       string          `json:"provider_label,omitempty"`
	Kind                string          `json:"kind,omitempty"`
	EmailAddress        string          `json:"email_address"`
	DisplayName         *string         `json:"display_name,omitempty"`
	AuthMode            string          `json:"auth_mode"`
	Settings            json.RawMessage `json:"settings"`
	TokenExpiresAt      *int64          `json:"token_expires_at,omitempty"`
	Status              string          `json:"status"`
	LastVerifiedAt      *int64          `json:"last_verified_at,omitempty"`
	LastError           *string         `json:"last_error,omitempty"`
	LastSyncedAt        *int64          `json:"last_synced_at,omitempty"`
	PollIntervalSeconds int             `json:"poll_interval_seconds"`
	NextPollAt          int64           `json:"next_poll_at"`
	CreatedAt           int64           `json:"created_at"`
	UpdatedAt           int64           `json:"updated_at"`
}

type FolderResponse struct {
	Name         string `json:"name"`
	MessageCount int    `json:"message_count"`
}

type VerifyMailAccountResponse struct {
	Status         string `json:"status"`
	FoldersFound   int    `json:"folders_found"`
	LastVerifiedAt int64  `json:"last_verified_at"`
}

type SyncMailAccountResponse struct {
	SyncRunID       string `json:"sync_run_id"`
	MessagesFetched int    `json:"messages_fetched"`
	MatchesCreated  int    `json:"matches_created"`
	Status          string `json:"status"`
}

type MailSyncRunResponse struct {
	ID              string  `json:"id"`
	Status          string  `json:"status"`
	StartedAt       int64   `json:"started_at"`
	FinishedAt      *int64  `json:"finished_at,omitempty"`
	MessagesFetched int     `json:"messages_fetched"`
	MatchesCreated  int     `json:"matches_created"`
	Error           *string `json:"error,omitempty"`
}

// CreateMailAccountRequest is provider-agnostic: Settings carries whatever the
// chosen provider's ConfigSchema asked for, which is why the SPA can render the
// connect form from GET /api/mail-provider-types without knowing about IMAP.
type CreateMailAccountRequest struct {
	UserID              string          `json:"-" validate:"required"`
	Provider            string          `json:"provider" validate:"required,max=40"`
	EmailAddress        string          `json:"email_address" validate:"required,email,max=320"`
	DisplayName         string          `json:"display_name" validate:"max=150"`
	AuthMode            string          `json:"auth_mode" validate:"omitempty,oneof=password app_password oauth2 xoauth2"`
	Settings            json.RawMessage `json:"settings"`
	Username            string          `json:"username" validate:"max=320"`
	Password            string          `json:"password" validate:"max=255"`
	PollIntervalSeconds int             `json:"poll_interval_seconds" validate:"omitempty,min=30,max=86400"`
}

type UpdateMailAccountRequest struct {
	UserID              string          `json:"-" validate:"required"`
	ID                  string          `json:"-" validate:"required"`
	DisplayName         string          `json:"display_name" validate:"max=150"`
	Settings            json.RawMessage `json:"settings"`
	Username            string          `json:"username" validate:"max=320"`
	Password            string          `json:"password" validate:"max=255"`
	PollIntervalSeconds int             `json:"poll_interval_seconds" validate:"omitempty,min=30,max=86400"`
	Status              string          `json:"status" validate:"omitempty,oneof=verified disabled"`
}

type GetMailAccountRequest struct {
	UserID string `json:"-" validate:"required"`
	ID     string `json:"-" validate:"required"`
}

type ListMailAccountRequest struct {
	PageRequest
	UserID   string `json:"-"`
	Status   string `json:"-" validate:"omitempty,oneof=pending verified error disabled"`
	Provider string `json:"-" validate:"max=40"`
}

type OAuthAuthorizeRequest struct {
	UserID   string `json:"-" validate:"required"`
	Provider string `json:"-" validate:"required,max=40"`
}

type OAuthAuthorizeResponse struct {
	RedirectURL string `json:"redirect_url"`
	State       string `json:"state"`
}

type OAuthCallbackRequest struct {
	Provider string `json:"-" validate:"required,max=40"`
	Code     string `json:"-" validate:"required"`
	State    string `json:"-" validate:"required"`
}

// MailAccountCredentials is what gets encrypted into mail_accounts.credentials.
type MailAccountCredentials struct {
	// Username is only needed when the server's login name differs from the
	// email address, which some hosts require.
	Username     string `json:"username,omitempty"`
	Password     string `json:"password,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

func (c MailAccountCredentials) Encode() (string, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// MailProviderResponse merges the database row (what a user may pick, and the
// host/port preset) with the registry descriptor (what that client can do).
// It is what GET /api/mail-provider-types serves.
type MailProviderResponse struct {
	Slug         string          `json:"slug"`
	Label        string          `json:"label"`
	Kind         string          `json:"kind"`
	AuthModes    []string        `json:"auth_modes"`
	Capabilities map[string]bool `json:"capabilities"`
	ConfigSchema Schema          `json:"config_schema"`
	Defaults     map[string]any  `json:"defaults,omitempty"`
	HelpURL      *string         `json:"help_url,omitempty"`
	Available    bool            `json:"available"`
	Unavailable  string          `json:"unavailable_reason,omitempty"`
}
