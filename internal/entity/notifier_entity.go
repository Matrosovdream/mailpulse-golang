package entity

const (
	NotifierStatusPending  = "pending"
	NotifierStatusVerified = "verified"
	NotifierStatusError    = "error"
	NotifierStatusDisabled = "disabled"
)

type Notifier struct {
	ID                    string  `gorm:"column:id;primaryKey"`
	UserID                string  `gorm:"column:user_id"`
	Type                  string  `gorm:"column:type"`
	Name                  string  `gorm:"column:name"`
	Config                JSON    `gorm:"column:config"`
	Secrets               *string `gorm:"column:secrets"`
	Status                string  `gorm:"column:status"`
	VerificationCode      *string `gorm:"column:verification_code"`
	VerificationExpiresAt *int64  `gorm:"column:verification_expires_at"`
	VerifiedAt            *int64  `gorm:"column:verified_at"`
	LastError             *string `gorm:"column:last_error"`
	IsDefault             bool    `gorm:"column:is_default"`
	CreatedAt             int64   `gorm:"column:created_at;autoCreateTime:milli"`
	UpdatedAt             int64   `gorm:"column:updated_at;autoCreateTime:milli;autoUpdateTime:milli"`
}

func (n *Notifier) TableName() string {
	return "notifiers"
}
