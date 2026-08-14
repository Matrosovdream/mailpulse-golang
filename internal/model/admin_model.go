package model

import "encoding/json"

type AdminUserResponse struct {
	UserResponse
	Counts AdminUserCounts `json:"counts"`
}

type AdminUserCounts struct {
	Watchers     int64 `json:"watchers"`
	MailAccounts int64 `json:"mail_accounts"`
	Notifiers    int64 `json:"notifiers"`
	Matches      int64 `json:"matches"`
}

type ListAdminUserRequest struct {
	PageRequest
	Query  string `json:"-" validate:"max=320"`
	Role   string `json:"-" validate:"max=50"`
	Status string `json:"-" validate:"omitempty,oneof=active suspended"`
}

type GetAdminUserRequest struct {
	ID string `json:"-" validate:"required"`
}

type UpdateAdminUserRequest struct {
	ActorID string   `json:"-" validate:"required"`
	ID      string   `json:"-" validate:"required"`
	Roles   []string `json:"roles" validate:"omitempty,dive,oneof=user superadmin"`
	Name    string   `json:"name" validate:"max=100"`
}

type SetAdminUserStatusRequest struct {
	ActorID string `json:"-" validate:"required"`
	ID      string `json:"-" validate:"required"`
	Status  string `json:"-" validate:"required,oneof=active suspended"`
}

type SuspendUserResponse struct {
	Status         string `json:"status"`
	WatchersPaused int64  `json:"watchers_paused"`
}

type ImpersonateRequest struct {
	ActorID   string `json:"-" validate:"required"`
	ID        string `json:"-" validate:"required"`
	UserAgent string `json:"-"`
	IP        string `json:"-"`
}

type ImpersonateResponse struct {
	Token         string        `json:"token"`
	ExpiresAt     int64         `json:"expires_at"`
	Impersonating *UserResponse `json:"impersonating"`
}

type AdminStatsResponse struct {
	Users             StatusCounts `json:"users"`
	WatchersActive    int64        `json:"watchers_active"`
	MailAccountsError int64        `json:"mail_accounts_error"`
	Queue             QueueStats   `json:"queue"`
	Runs24h           StatusCounts `json:"runs_24h"`
}

type QueueStats struct {
	PendingRuns          int64 `json:"pending_runs"`
	OverdueRuns          int64 `json:"overdue_runs"`
	OldestPendingSeconds int64 `json:"oldest_pending_seconds"`
}

type AuditLogResponse struct {
	ID                 string          `json:"id"`
	ActorUserID        *string         `json:"actor_user_id,omitempty"`
	ActorEmail         string          `json:"actor_email,omitempty"`
	ImpersonatedUserID *string         `json:"impersonated_user_id,omitempty"`
	Action             string          `json:"action"`
	EntityType         string          `json:"entity_type"`
	EntityID           *string         `json:"entity_id,omitempty"`
	Metadata           json.RawMessage `json:"metadata,omitempty"`
	IP                 *string         `json:"ip,omitempty"`
	CreatedAt          int64           `json:"created_at"`
}

type ListAuditLogRequest struct {
	PageRequest
	ActorUserID string `json:"-"`
	Action      string `json:"-" validate:"max=80"`
	EntityType  string `json:"-" validate:"max=50"`
	From        int64  `json:"-"`
	To          int64  `json:"-"`
}

type HealthResponse struct {
	Database string `json:"database"`
	Redis    string `json:"redis"`
	Kafka    string `json:"kafka"`
	Version  string `json:"version"`
}
