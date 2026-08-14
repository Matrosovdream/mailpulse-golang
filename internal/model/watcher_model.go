package model

type WatcherResponse struct {
	ID              string                  `json:"id"`
	Name            string                  `json:"name"`
	Description     *string                 `json:"description,omitempty"`
	Status          string                  `json:"status"`
	MatchMode       string                  `json:"match_mode"`
	Folder          string                  `json:"folder"`
	WatchFrom       *int64                  `json:"watch_from,omitempty"`
	CooldownSeconds int                     `json:"cooldown_seconds"`
	LastMatchedAt   *int64                  `json:"last_matched_at,omitempty"`
	MatchCount      int64                   `json:"match_count"`
	ArchivedAt      *int64                  `json:"archived_at,omitempty"`
	MailAccountID   string                  `json:"mail_account_id"`
	MailAccount     *MailAccountResponse    `json:"mail_account,omitempty"`
	Filters         []WatcherFilterResponse `json:"filters,omitempty"`
	Events          []WatcherEventResponse  `json:"events,omitempty"`
	CreatedAt       int64                   `json:"created_at"`
	UpdatedAt       int64                   `json:"updated_at"`
}

type WatcherFilterResponse struct {
	ID            string  `json:"id"`
	Field         string  `json:"field"`
	HeaderName    *string `json:"header_name,omitempty"`
	Operator      string  `json:"operator"`
	Value         string  `json:"value"`
	CaseSensitive bool    `json:"case_sensitive"`
	Position      int     `json:"position"`
}

type WatcherFilterRequest struct {
	Field         string `json:"field" validate:"required,oneof=subject from to cc body header attachment_name has_attachment size"`
	HeaderName    string `json:"header_name" validate:"max=100"`
	Operator      string `json:"operator" validate:"required,oneof=contains not_contains equals starts_with ends_with regex gt lt exists"`
	Value         string `json:"value" validate:"required"`
	CaseSensitive bool   `json:"case_sensitive"`
	Position      int    `json:"position"`
}

type CreateWatcherRequest struct {
	UserID          string                      `json:"-" validate:"required"`
	MailAccountID   string                      `json:"mail_account_id" validate:"required"`
	Name            string                      `json:"name" validate:"required,max=150"`
	Description     string                      `json:"description"`
	MatchMode       string                      `json:"match_mode" validate:"omitempty,oneof=all any"`
	Folder          string                      `json:"folder" validate:"max=255"`
	WatchFrom       *int64                      `json:"watch_from"`
	CooldownSeconds int                         `json:"cooldown_seconds" validate:"min=0"`
	Filters         []WatcherFilterRequest      `json:"filters" validate:"dive"`
	Events          []CreateWatcherEventRequest `json:"events" validate:"dive"`
}

type UpdateWatcherRequest struct {
	UserID          string  `json:"-" validate:"required"`
	ID              string  `json:"-" validate:"required"`
	Name            string  `json:"name" validate:"max=150"`
	Description     *string `json:"description"`
	MatchMode       string  `json:"match_mode" validate:"omitempty,oneof=all any"`
	Folder          string  `json:"folder" validate:"max=255"`
	CooldownSeconds *int    `json:"cooldown_seconds"`
}

type GetWatcherRequest struct {
	UserID string `json:"-" validate:"required"`
	ID     string `json:"-" validate:"required"`
}

type ListWatcherRequest struct {
	PageRequest
	UserID        string `json:"-"`
	Status        string `json:"-" validate:"omitempty,oneof=active paused archived all"`
	MailAccountID string `json:"-"`
	Query         string `json:"-" validate:"max=150"`
}

// SetWatcherStatusRequest backs _archive, _restore, _pause and _resume.
type SetWatcherStatusRequest struct {
	UserID string `json:"-" validate:"required"`
	ID     string `json:"-" validate:"required"`
	Status string `json:"-" validate:"required,oneof=active paused archived"`
}

type ReplaceFiltersRequest struct {
	UserID    string                 `json:"-" validate:"required"`
	WatcherID string                 `json:"-" validate:"required"`
	MatchMode string                 `json:"match_mode" validate:"omitempty,oneof=all any"`
	Filters   []WatcherFilterRequest `json:"filters" validate:"dive"`
}

type TestWatcherRequest struct {
	UserID     string `json:"-" validate:"required"`
	ID         string `json:"-" validate:"required"`
	SampleSize int    `json:"sample_size" validate:"omitempty,min=1,max=200"`
}

type TestWatcherResponse struct {
	Scanned int                 `json:"scanned"`
	Matched int                 `json:"matched"`
	Samples []TestWatcherSample `json:"samples"`
}

type TestWatcherSample struct {
	Subject        string   `json:"subject"`
	FromAddress    string   `json:"from_address"`
	ReceivedAt     int64    `json:"received_at"`
	Matched        bool     `json:"matched"`
	MatchedFilters []string `json:"matched_filters"`
}

type WatcherStatsRequest struct {
	UserID string `json:"-" validate:"required"`
	ID     string `json:"-" validate:"required"`
	Days   int    `json:"-" validate:"omitempty,min=1,max=365"`
}

type WatcherStatsResponse struct {
	TotalMatches  int64             `json:"total_matches"`
	RunsSucceeded int64             `json:"runs_succeeded"`
	RunsFailed    int64             `json:"runs_failed"`
	RunsPending   int64             `json:"runs_pending"`
	Daily         []DailyMatchCount `json:"daily"`
}

type DailyMatchCount struct {
	Date    string `json:"date"`
	Matches int64  `json:"matches"`
}
