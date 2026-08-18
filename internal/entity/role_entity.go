package entity

// Role slugs seeded by 20260812000100_create_table_roles.
const (
	RoleUser       = "user"
	RoleSuperadmin = "superadmin"
)

type Role struct {
	ID          string  `gorm:"column:id;primaryKey"`
	Slug        string  `gorm:"column:slug"`
	Name        string  `gorm:"column:name"`
	Description *string `gorm:"column:description"`
	CreatedAt   int64   `gorm:"column:created_at;autoCreateTime:milli"`
	UpdatedAt   int64   `gorm:"column:updated_at;autoCreateTime:milli;autoUpdateTime:milli"`
}

func (r *Role) TableName() string {
	return "roles"
}

type UserRole struct {
	UserID    string `gorm:"column:user_id;primaryKey"`
	RoleID    string `gorm:"column:role_id;primaryKey"`
	CreatedAt int64  `gorm:"column:created_at;autoCreateTime:milli"`
}

func (r *UserRole) TableName() string {
	return "user_roles"
}
