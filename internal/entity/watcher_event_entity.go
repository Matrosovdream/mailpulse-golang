package entity

const (
	RunModeImmediate = "immediate"
	RunModeDelayed   = "delayed"
	RunModeRecurring = "recurring"
)

type WatcherEvent struct {
	ID                    string  `gorm:"column:id;primaryKey"`
	WatcherID             string  `gorm:"column:watcher_id"`
	Type                  string  `gorm:"column:type"`
	Config                JSON    `gorm:"column:config"`
	Position              int     `gorm:"column:position"`
	Enabled               bool    `gorm:"column:enabled"`
	RunMode               string  `gorm:"column:run_mode"`
	DelaySeconds          int     `gorm:"column:delay_seconds"`
	RepeatIntervalSeconds *int    `gorm:"column:repeat_interval_seconds"`
	RepeatMax             *int    `gorm:"column:repeat_max"`
	RepeatUntil           *int64  `gorm:"column:repeat_until"`
	CronExpression        *string `gorm:"column:cron_expression"`
	StopOnAck             bool    `gorm:"column:stop_on_ack"`
	CreatedAt             int64   `gorm:"column:created_at;autoCreateTime:milli"`
	UpdatedAt             int64   `gorm:"column:updated_at;autoCreateTime:milli;autoUpdateTime:milli"`

	Notifiers []Notifier `gorm:"-"`
}

func (e *WatcherEvent) TableName() string {
	return "watcher_events"
}

type WatcherEventNotifier struct {
	WatcherEventID string `gorm:"column:watcher_event_id;primaryKey"`
	NotifierID     string `gorm:"column:notifier_id;primaryKey"`
	Position       int    `gorm:"column:position"`
	CreatedAt      int64  `gorm:"column:created_at;autoCreateTime:milli"`
}

func (e *WatcherEventNotifier) TableName() string {
	return "watcher_event_notifiers"
}
