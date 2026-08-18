package entity

const (
	WatcherStatusActive   = "active"
	WatcherStatusPaused   = "paused"
	WatcherStatusArchived = "archived"

	MatchModeAll = "all"
	MatchModeAny = "any"
)

type Watcher struct {
	ID              string  `gorm:"column:id;primaryKey"`
	UserID          string  `gorm:"column:user_id"`
	MailAccountID   string  `gorm:"column:mail_account_id"`
	Name            string  `gorm:"column:name"`
	Description     *string `gorm:"column:description"`
	Status          string  `gorm:"column:status"`
	MatchMode       string  `gorm:"column:match_mode"`
	Folder          string  `gorm:"column:folder"`
	WatchFrom       *int64  `gorm:"column:watch_from"`
	CooldownSeconds int     `gorm:"column:cooldown_seconds"`
	LastMatchedAt   *int64  `gorm:"column:last_matched_at"`
	MatchCount      int64   `gorm:"column:match_count"`
	ArchivedAt      *int64  `gorm:"column:archived_at"`
	CreatedAt       int64   `gorm:"column:created_at;autoCreateTime:milli"`
	UpdatedAt       int64   `gorm:"column:updated_at;autoCreateTime:milli;autoUpdateTime:milli"`

	Filters     []WatcherFilter `gorm:"-"`
	Events      []WatcherEvent  `gorm:"-"`
	MailAccount *MailAccount    `gorm:"-"`
}

func (w *Watcher) TableName() string {
	return "watchers"
}

// Filter fields and operators. The catalog usecase serves these to the SPA so
// the filter builder never hard-codes a list that can drift from the database
// check constraints.
const (
	FieldSubject        = "subject"
	FieldFrom           = "from"
	FieldTo             = "to"
	FieldCc             = "cc"
	FieldBody           = "body"
	FieldHeader         = "header"
	FieldAttachmentName = "attachment_name"
	FieldHasAttachment  = "has_attachment"
	FieldSize           = "size"

	OpContains    = "contains"
	OpNotContains = "not_contains"
	OpEquals      = "equals"
	OpStartsWith  = "starts_with"
	OpEndsWith    = "ends_with"
	OpRegex       = "regex"
	OpGt          = "gt"
	OpLt          = "lt"
	OpExists      = "exists"
)

type WatcherFilter struct {
	ID            string  `gorm:"column:id;primaryKey"`
	WatcherID     string  `gorm:"column:watcher_id"`
	Field         string  `gorm:"column:field"`
	HeaderName    *string `gorm:"column:header_name"`
	Operator      string  `gorm:"column:operator"`
	Value         string  `gorm:"column:value"`
	CaseSensitive bool    `gorm:"column:case_sensitive"`
	Position      int     `gorm:"column:position"`
	CreatedAt     int64   `gorm:"column:created_at;autoCreateTime:milli"`
	UpdatedAt     int64   `gorm:"column:updated_at;autoCreateTime:milli;autoUpdateTime:milli"`
}

func (f *WatcherFilter) TableName() string {
	return "watcher_filters"
}
