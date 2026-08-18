package entity

// User is an account. Roles are not a gorm association: the user_roles join
// table carries its own created_at, which gorm's many2many writer would leave
// unset, so the repository loads and writes the join rows explicitly.
type User struct {
	ID              string `gorm:"column:id;primaryKey"`
	Email           string `gorm:"column:email"`
	Name            string `gorm:"column:name"`
	Password        string `gorm:"column:password"`
	Status          string `gorm:"column:status"`
	Timezone        string `gorm:"column:timezone"`
	EmailVerifiedAt *int64 `gorm:"column:email_verified_at"`
	CreatedAt       int64  `gorm:"column:created_at;autoCreateTime:milli"`
	UpdatedAt       int64  `gorm:"column:updated_at;autoCreateTime:milli;autoUpdateTime:milli"`

	Roles []Role `gorm:"-"`
}

func (u *User) TableName() string {
	return "users"
}

const (
	UserStatusActive    = "active"
	UserStatusSuspended = "suspended"
)

// HasRole reports whether the loaded roles contain the given slug.
func (u *User) HasRole(slug string) bool {
	for _, role := range u.Roles {
		if role.Slug == slug {
			return true
		}
	}
	return false
}

func (u *User) RoleSlugs() []string {
	slugs := make([]string, 0, len(u.Roles))
	for _, role := range u.Roles {
		slugs = append(slugs, role.Slug)
	}
	return slugs
}
