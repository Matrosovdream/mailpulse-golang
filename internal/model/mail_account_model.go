package model

import "encoding/json"

type MailAccountResponse struct {
	ID                  string  `json:"id"`
	Provider            string  `json:"provider"`
	EmailAddress        string  `json:"email_address"`
	DisplayName         *string `json:"display_name,omitempty"`
	AuthType            string  `json:"auth_type"`
	ImapHost            *string `json:"imap_host,omitempty"`
	ImapPort            *int    `json:"imap_port,omitempty"`
	ImapUseTLS          bool    `json:"imap_use_tls"`
	Status              string  `json:"status"`
	LastVerifiedAt      *int64  `json:"last_verified_at,omitempty"`
	LastError           *string `json:"last_error,omitempty"`
	LastSyncedAt        *int64  `json:"last_synced_at,omitempty"`
	PollIntervalSeconds int     `json:"poll_interval_seconds"`
	NextPollAt          int64   `json:"next_poll_at"`
	CreatedAt           int64   `json:"created_at"`
	UpdatedAt           int64   `json:"updated_at"`
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

type CreateMailAccountRequest struct {
	UserID              string `json:"-" validate:"required"`
	Provider            string `json:"provider" validate:"required,oneof=gmail outlook imap"`
	EmailAddress        string `json:"email_address" validate:"required,email,max=320"`
	DisplayName         string `json:"display_name" validate:"max=150"`
	AuthType            string `json:"auth_type" validate:"required,oneof=oauth2 password"`
	Password            string `json:"password" validate:"max=255"`
	ImapHost            string `json:"imap_host" validate:"max=255"`
	ImapPort            int    `json:"imap_port" validate:"omitempty,min=1,max=65535"`
	ImapUseTLS          *bool  `json:"imap_use_tls"`
	PollIntervalSeconds int    `json:"poll_interval_seconds" validate:"omitempty,min=30,max=86400"`
}

type UpdateMailAccountRequest struct {
	UserID              string `json:"-" validate:"required"`
	ID                  string `json:"-" validate:"required"`
	DisplayName         string `json:"display_name" validate:"max=150"`
	Password            string `json:"password" validate:"max=255"`
	ImapHost            string `json:"imap_host" validate:"max=255"`
	ImapPort            int    `json:"imap_port" validate:"omitempty,min=1,max=65535"`
	PollIntervalSeconds int    `json:"poll_interval_seconds" validate:"omitempty,min=30,max=86400"`
	Status              string `json:"status" validate:"omitempty,oneof=verified disabled"`
}

type GetMailAccountRequest struct {
	UserID string `json:"-" validate:"required"`
	ID     string `json:"-" validate:"required"`
}

type ListMailAccountRequest struct {
	PageRequest
	UserID   string `json:"-"`
	Status   string `json:"-" validate:"omitempty,oneof=pending verified error disabled"`
	Provider string `json:"-" validate:"omitempty,oneof=gmail outlook imap"`
}

type OAuthAuthorizeRequest struct {
	UserID   string `json:"-" validate:"required"`
	Provider string `json:"-" validate:"required,oneof=gmail outlook"`
}

type OAuthAuthorizeResponse struct {
	RedirectURL string `json:"redirect_url"`
	State       string `json:"state"`
}

type OAuthCallbackRequest struct {
	Provider string `json:"-" validate:"required,oneof=gmail outlook"`
	Code     string `json:"-" validate:"required"`
	State    string `json:"-" validate:"required"`
}

// MailAccountCredentials is what gets encrypted into mail_accounts.credentials.
type MailAccountCredentials struct {
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
