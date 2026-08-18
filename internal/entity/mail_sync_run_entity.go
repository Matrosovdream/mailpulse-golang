package entity

const (
	SyncStatusRunning = "running"
	SyncStatusOK      = "ok"
	SyncStatusError   = "error"
)

type MailSyncRun struct {
	ID              string  `gorm:"column:id;primaryKey"`
	MailAccountID   string  `gorm:"column:mail_account_id"`
	Status          string  `gorm:"column:status"`
	StartedAt       int64   `gorm:"column:started_at"`
	FinishedAt      *int64  `gorm:"column:finished_at"`
	MessagesFetched int     `gorm:"column:messages_fetched"`
	MatchesCreated  int     `gorm:"column:matches_created"`
	Error           *string `gorm:"column:error"`
	CreatedAt       int64   `gorm:"column:created_at;autoCreateTime:milli"`
	UpdatedAt       int64   `gorm:"column:updated_at;autoCreateTime:milli;autoUpdateTime:milli"`
}

func (m *MailSyncRun) TableName() string {
	return "mail_sync_runs"
}
