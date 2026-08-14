package model

import "encoding/json"

type MatchedEmailResponse struct {
	ID             string             `json:"id"`
	WatcherID      string             `json:"watcher_id"`
	WatcherName    string             `json:"watcher_name,omitempty"`
	MailAccountID  string             `json:"mail_account_id"`
	MessageID      string             `json:"message_id"`
	Subject        *string            `json:"subject,omitempty"`
	FromAddress    *string            `json:"from_address,omitempty"`
	FromName       *string            `json:"from_name,omitempty"`
	ToAddresses    *string            `json:"to_addresses,omitempty"`
	Snippet        *string            `json:"snippet,omitempty"`
	HasAttachment  bool               `json:"has_attachment"`
	SizeBytes      int                `json:"size_bytes"`
	ReceivedAt     int64              `json:"received_at"`
	MatchedAt      int64              `json:"matched_at"`
	MatchedFilters json.RawMessage    `json:"matched_filters,omitempty"`
	Runs           []EventRunResponse `json:"runs,omitempty"`
}

type EventRunResponse struct {
	ID             string             `json:"id"`
	MatchedEmailID string             `json:"matched_email_id"`
	WatcherEventID string             `json:"watcher_event_id"`
	EventType      string             `json:"event_type,omitempty"`
	Occurrence     int                `json:"occurrence"`
	Status         string             `json:"status"`
	ScheduledAt    int64              `json:"scheduled_at"`
	StartedAt      *int64             `json:"started_at,omitempty"`
	FinishedAt     *int64             `json:"finished_at,omitempty"`
	Attempt        int                `json:"attempt"`
	MaxAttempts    int                `json:"max_attempts"`
	ConfigSnapshot json.RawMessage    `json:"config_snapshot,omitempty"`
	Result         json.RawMessage    `json:"result,omitempty"`
	Error          *string            `json:"error,omitempty"`
	AcknowledgedAt *int64             `json:"acknowledged_at,omitempty"`
	Deliveries     []DeliveryResponse `json:"deliveries,omitempty"`
	CreatedAt      int64              `json:"created_at"`
}

type DeliveryResponse struct {
	ID                string  `json:"id"`
	EventRunID        string  `json:"event_run_id"`
	NotifierID        *string `json:"notifier_id,omitempty"`
	ChannelType       string  `json:"channel_type"`
	Status            string  `json:"status"`
	RenderedMessage   *string `json:"rendered_message,omitempty"`
	ProviderMessageID *string `json:"provider_message_id,omitempty"`
	Error             *string `json:"error,omitempty"`
	SentAt            *int64  `json:"sent_at,omitempty"`
	CreatedAt         int64   `json:"created_at"`
}

type ListMatchedEmailRequest struct {
	PageRequest
	UserID        string `json:"-"`
	WatcherID     string `json:"-"`
	MailAccountID string `json:"-"`
	From          int64  `json:"-"`
	To            int64  `json:"-"`
	Query         string `json:"-" validate:"max=200"`
}

type GetMatchedEmailRequest struct {
	UserID string `json:"-"`
	ID     string `json:"-" validate:"required"`
}

type ListEventRunRequest struct {
	PageRequest
	UserID    string `json:"-"`
	WatcherID string `json:"-"`
	Status    string `json:"-" validate:"omitempty,oneof=pending running succeeded failed cancelled skipped"`
	From      int64  `json:"-"`
	To        int64  `json:"-"`
}

type GetEventRunRequest struct {
	UserID string `json:"-"`
	ID     string `json:"-" validate:"required"`
}

type EventRunActionRequest struct {
	UserID string `json:"-" validate:"required"`
	ID     string `json:"-" validate:"required"`
}

type CancelRunResponse struct {
	Cancelled int64 `json:"cancelled"`
}

type AckRunResponse struct {
	AcknowledgedAt       int64 `json:"acknowledged_at"`
	CancelledOccurrences int64 `json:"cancelled_occurrences"`
}

type ListDeliveryRequest struct {
	PageRequest
	UserID     string `json:"-"`
	EventRunID string `json:"-"`
	NotifierID string `json:"-"`
	Status     string `json:"-" validate:"omitempty,oneof=pending sent failed"`
}

type DashboardSummaryRequest struct {
	UserID string `json:"-" validate:"required"`
}

type DashboardSummaryResponse struct {
	Watchers      StatusCounts     `json:"watchers"`
	MailAccounts  StatusCounts     `json:"mail_accounts"`
	Notifiers     StatusCounts     `json:"notifiers"`
	Matches24h    int64            `json:"matches_24h"`
	RunsFailed24h int64            `json:"runs_failed_24h"`
	Recent        []RecentActivity `json:"recent"`
}

type StatusCounts map[string]int64

type RecentActivity struct {
	Type        string `json:"type"`
	ID          string `json:"id"`
	WatcherName string `json:"watcher_name,omitempty"`
	Subject     string `json:"subject,omitempty"`
	Status      string `json:"status,omitempty"`
	At          int64  `json:"at"`
}
