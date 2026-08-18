package entity

const (
	MailAccountStatusPending  = "pending"
	MailAccountStatusVerified = "verified"
	MailAccountStatusError    = "error"
	MailAccountStatusDisabled = "disabled"
)

// MailAccount is one connected mailbox.
//
// Connection details live in Settings rather than in columns: an API provider
// has no host or port, and an IMAP one has no tenant id. Provider is a slug
// referencing mail_providers, which names the client that serves it.
type MailAccount struct {
	ID                  string  `gorm:"column:id;primaryKey"`
	UserID              string  `gorm:"column:user_id"`
	Provider            string  `gorm:"column:provider"`
	EmailAddress        string  `gorm:"column:email_address"`
	DisplayName         *string `gorm:"column:display_name"`
	AuthMode            string  `gorm:"column:auth_mode"`
	Credentials         string  `gorm:"column:credentials"`
	Settings            JSON    `gorm:"column:settings"`
	ProviderAccountID   *string `gorm:"column:provider_account_id"`
	Scopes              *string `gorm:"column:scopes"`
	TokenExpiresAt      *int64  `gorm:"column:token_expires_at"`
	Status              string  `gorm:"column:status"`
	LastVerifiedAt      *int64  `gorm:"column:last_verified_at"`
	LastError           *string `gorm:"column:last_error"`
	SyncState           JSON    `gorm:"column:sync_state"`
	LastSyncedAt        *int64  `gorm:"column:last_synced_at"`
	LastPushAt          *int64  `gorm:"column:last_push_at"`
	PollIntervalSeconds int     `gorm:"column:poll_interval_seconds"`
	NextPollAt          int64   `gorm:"column:next_poll_at"`
	CreatedAt           int64   `gorm:"column:created_at;autoCreateTime:milli"`
	UpdatedAt           int64   `gorm:"column:updated_at;autoCreateTime:milli;autoUpdateTime:milli"`
}

func (m *MailAccount) TableName() string {
	return "mail_accounts"
}
