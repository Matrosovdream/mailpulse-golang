package model

import "encoding/json"

type WatcherEventResponse struct {
	ID                    string             `json:"id"`
	WatcherID             string             `json:"watcher_id"`
	Type                  string             `json:"type"`
	Config                json.RawMessage    `json:"config"`
	Position              int                `json:"position"`
	Enabled               bool               `json:"enabled"`
	RunMode               string             `json:"run_mode"`
	DelaySeconds          int                `json:"delay_seconds"`
	RepeatIntervalSeconds *int               `json:"repeat_interval_seconds,omitempty"`
	RepeatMax             *int               `json:"repeat_max,omitempty"`
	RepeatUntil           *int64             `json:"repeat_until,omitempty"`
	CronExpression        *string            `json:"cron_expression,omitempty"`
	StopOnAck             bool               `json:"stop_on_ack"`
	Notifiers             []NotifierResponse `json:"notifiers,omitempty"`
	CreatedAt             int64              `json:"created_at"`
	UpdatedAt             int64              `json:"updated_at"`
}

type CreateWatcherEventRequest struct {
	UserID                string          `json:"-"`
	WatcherID             string          `json:"-"`
	Type                  string          `json:"type" validate:"required,max=40"`
	Config                json.RawMessage `json:"config"`
	Position              int             `json:"position"`
	Enabled               *bool           `json:"enabled"`
	RunMode               string          `json:"run_mode" validate:"omitempty,oneof=immediate delayed recurring"`
	DelaySeconds          int             `json:"delay_seconds" validate:"min=0"`
	RepeatIntervalSeconds *int            `json:"repeat_interval_seconds" validate:"omitempty,min=1"`
	RepeatMax             *int            `json:"repeat_max" validate:"omitempty,min=1"`
	RepeatUntil           *int64          `json:"repeat_until"`
	CronExpression        string          `json:"cron_expression" validate:"max=120"`
	StopOnAck             bool            `json:"stop_on_ack"`
	NotifierIDs           []string        `json:"notifier_ids"`
}

type UpdateWatcherEventRequest struct {
	UserID                string          `json:"-" validate:"required"`
	WatcherID             string          `json:"-" validate:"required"`
	ID                    string          `json:"-" validate:"required"`
	Config                json.RawMessage `json:"config,omitempty"`
	Position              *int            `json:"position"`
	Enabled               *bool           `json:"enabled"`
	RunMode               string          `json:"run_mode" validate:"omitempty,oneof=immediate delayed recurring"`
	DelaySeconds          *int            `json:"delay_seconds"`
	RepeatIntervalSeconds *int            `json:"repeat_interval_seconds" validate:"omitempty,min=1"`
	RepeatMax             *int            `json:"repeat_max" validate:"omitempty,min=1"`
	RepeatUntil           *int64          `json:"repeat_until"`
	CronExpression        *string         `json:"cron_expression" validate:"omitempty,max=120"`
	StopOnAck             *bool           `json:"stop_on_ack"`
	NotifierIDs           []string        `json:"notifier_ids"`
}

type GetWatcherEventRequest struct {
	UserID    string `json:"-" validate:"required"`
	WatcherID string `json:"-" validate:"required"`
	ID        string `json:"-" validate:"required"`
}

type ListWatcherEventRequest struct {
	UserID    string `json:"-" validate:"required"`
	WatcherID string `json:"-" validate:"required"`
}

type ReorderWatcherEventRequest struct {
	UserID    string   `json:"-" validate:"required"`
	WatcherID string   `json:"-" validate:"required"`
	EventIDs  []string `json:"event_ids" validate:"required,min=1,dive,required"`
}

type TestWatcherEventRequest struct {
	UserID         string `json:"-" validate:"required"`
	WatcherID      string `json:"-" validate:"required"`
	ID             string `json:"-" validate:"required"`
	MatchedEmailID string `json:"matched_email_id"`
}
