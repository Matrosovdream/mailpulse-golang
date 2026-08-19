package entity

type UserSession struct {
	ID        string  `gorm:"column:id;primaryKey"`
	UserID    string  `gorm:"column:user_id"`
	TokenHash string  `gorm:"column:token_hash"`
	UserAgent *string `gorm:"column:user_agent"`
	IP        *string `gorm:"column:ip"`
	// set only on sessions issued by admin impersonation; it names the admin,
	// while UserID stays the target so ownership checks still scope to them
	ImpersonatedBy *string `gorm:"column:impersonated_by"`
	ExpiresAt      int64   `gorm:"column:expires_at"`
	RevokedAt      *int64  `gorm:"column:revoked_at"`
	LastUsedAt     int64   `gorm:"column:last_used_at"`
	CreatedAt  int64   `gorm:"column:created_at;autoCreateTime:milli"`
	UpdatedAt  int64   `gorm:"column:updated_at;autoCreateTime:milli;autoUpdateTime:milli"`
}

func (s *UserSession) TableName() string {
	return "user_sessions"
}
