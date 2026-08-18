package entity

const (
	RunStatusPending   = "pending"
	RunStatusRunning   = "running"
	RunStatusSucceeded = "succeeded"
	RunStatusFailed    = "failed"
	RunStatusCancelled = "cancelled"
	RunStatusSkipped   = "skipped"

	DeliveryStatusPending = "pending"
	DeliveryStatusSent    = "sent"
	DeliveryStatusFailed  = "failed"
)

type EventRun struct {
	ID             string  `gorm:"column:id;primaryKey"`
	UserID         string  `gorm:"column:user_id"`
	MatchedEmailID string  `gorm:"column:matched_email_id"`
	WatcherEventID string  `gorm:"column:watcher_event_id"`
	Occurrence     int     `gorm:"column:occurrence"`
	Status         string  `gorm:"column:status"`
	ScheduledAt    int64   `gorm:"column:scheduled_at"`
	StartedAt      *int64  `gorm:"column:started_at"`
	FinishedAt     *int64  `gorm:"column:finished_at"`
	Attempt        int     `gorm:"column:attempt"`
	MaxAttempts    int     `gorm:"column:max_attempts"`
	NextRetryAt    *int64  `gorm:"column:next_retry_at"`
	ConfigSnapshot JSON    `gorm:"column:config_snapshot"`
	Result         JSON    `gorm:"column:result"`
	Error          *string `gorm:"column:error"`
	AcknowledgedAt *int64  `gorm:"column:acknowledged_at"`
	CreatedAt      int64   `gorm:"column:created_at;autoCreateTime:milli"`
	UpdatedAt      int64   `gorm:"column:updated_at;autoCreateTime:milli;autoUpdateTime:milli"`

	Deliveries []NotificationDelivery `gorm:"-"`
}

func (r *EventRun) TableName() string {
	return "event_runs"
}

type NotificationDelivery struct {
	ID                string  `gorm:"column:id;primaryKey"`
	EventRunID        string  `gorm:"column:event_run_id"`
	NotifierID        *string `gorm:"column:notifier_id"`
	ChannelType       string  `gorm:"column:channel_type"`
	Status            string  `gorm:"column:status"`
	RenderedMessage   *string `gorm:"column:rendered_message"`
	ProviderMessageID *string `gorm:"column:provider_message_id"`
	Error             *string `gorm:"column:error"`
	SentAt            *int64  `gorm:"column:sent_at"`
	CreatedAt         int64   `gorm:"column:created_at;autoCreateTime:milli"`
	UpdatedAt         int64   `gorm:"column:updated_at;autoCreateTime:milli;autoUpdateTime:milli"`
}

func (d *NotificationDelivery) TableName() string {
	return "notification_deliveries"
}
