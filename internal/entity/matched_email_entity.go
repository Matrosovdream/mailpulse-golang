package entity

type MatchedEmail struct {
	ID             string  `gorm:"column:id;primaryKey"`
	UserID         string  `gorm:"column:user_id"`
	WatcherID      string  `gorm:"column:watcher_id"`
	MailAccountID  string  `gorm:"column:mail_account_id"`
	MessageID      string  `gorm:"column:message_id"`
	ProviderUID    string  `gorm:"column:provider_uid"`
	Subject        *string `gorm:"column:subject"`
	FromAddress    *string `gorm:"column:from_address"`
	FromName       *string `gorm:"column:from_name"`
	ToAddresses    *string `gorm:"column:to_addresses"`
	Snippet        *string `gorm:"column:snippet"`
	HasAttachment  bool    `gorm:"column:has_attachment"`
	SizeBytes      int     `gorm:"column:size_bytes"`
	ReceivedAt     int64   `gorm:"column:received_at"`
	MatchedAt      int64   `gorm:"column:matched_at"`
	MatchedFilters JSON    `gorm:"column:matched_filters"`
	CreatedAt      int64   `gorm:"column:created_at;autoCreateTime:milli"`
	UpdatedAt      int64   `gorm:"column:updated_at;autoCreateTime:milli;autoUpdateTime:milli"`

	Runs []EventRun `gorm:"-"`
}

func (m *MatchedEmail) TableName() string {
	return "matched_emails"
}
