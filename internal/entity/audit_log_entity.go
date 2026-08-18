package entity

type AuditLog struct {
	ID                 string  `gorm:"column:id;primaryKey"`
	ActorUserID        *string `gorm:"column:actor_user_id"`
	ImpersonatedUserID *string `gorm:"column:impersonated_user_id"`
	Action             string  `gorm:"column:action"`
	EntityType         string  `gorm:"column:entity_type"`
	EntityID           *string `gorm:"column:entity_id"`
	Metadata           JSON    `gorm:"column:metadata"`
	IP                 *string `gorm:"column:ip"`
	UserAgent          *string `gorm:"column:user_agent"`
	CreatedAt          int64   `gorm:"column:created_at;autoCreateTime:milli"`
}

func (a *AuditLog) TableName() string {
	return "audit_logs"
}
