package repository

import (
	"mailpulse/internal/entity"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type UserRepository struct {
	Repository[entity.User]
	Log *logrus.Logger
}

func NewUserRepository(log *logrus.Logger) *UserRepository {
	return &UserRepository{Log: log}
}

func (r *UserRepository) FindByEmail(db *gorm.DB, user *entity.User, email string) error {
	return db.Where("email = ?", email).Take(user).Error
}

func (r *UserRepository) CountByEmail(db *gorm.DB, email string) (int64, error) {
	var total int64
	err := db.Model(&entity.User{}).Where("email = ?", email).Count(&total).Error
	return total, err
}

// LoadRoles fills User.Roles through the user_roles pivot.
func (r *UserRepository) LoadRoles(db *gorm.DB, user *entity.User) error {
	var roles []entity.Role
	err := db.Model(&entity.Role{}).
		Joins("JOIN user_roles ON user_roles.role_id = roles.id").
		Where("user_roles.user_id = ?", user.ID).
		Order("roles.slug ASC").
		Find(&roles).Error
	if err != nil {
		return err
	}
	user.Roles = roles
	return nil
}

// LoadRolesForAll avoids the N+1 when rendering an admin user list.
func (r *UserRepository) LoadRolesForAll(db *gorm.DB, users []entity.User) error {
	if len(users) == 0 {
		return nil
	}

	ids := make([]string, len(users))
	for i := range users {
		ids[i] = users[i].ID
	}

	type row struct {
		UserID string
		entity.Role
	}

	var rows []row
	err := db.Table("roles").
		Select("user_roles.user_id AS user_id, roles.*").
		Joins("JOIN user_roles ON user_roles.role_id = roles.id").
		Where("user_roles.user_id IN ?", ids).
		Order("roles.slug ASC").
		Scan(&rows).Error
	if err != nil {
		return err
	}

	byUser := make(map[string][]entity.Role, len(users))
	for _, item := range rows {
		byUser[item.UserID] = append(byUser[item.UserID], item.Role)
	}

	for i := range users {
		users[i].Roles = byUser[users[i].ID]
	}

	return nil
}

func (r *UserRepository) ReplaceRoles(db *gorm.DB, userID string, roleIDs []string) error {
	if err := db.Where("user_id = ?", userID).Delete(&entity.UserRole{}).Error; err != nil {
		return err
	}

	if len(roleIDs) == 0 {
		return nil
	}

	links := make([]entity.UserRole, 0, len(roleIDs))
	for _, roleID := range roleIDs {
		links = append(links, entity.UserRole{UserID: userID, RoleID: roleID})
	}

	return db.Create(&links).Error
}

func (r *UserRepository) AddRole(db *gorm.DB, userID, roleID string) error {
	return db.Create(&entity.UserRole{UserID: userID, RoleID: roleID}).Error
}

func (r *UserRepository) Search(db *gorm.DB, query, role, status string) *gorm.DB {
	tx := db.Model(&entity.User{})

	if query != "" {
		like := "%" + query + "%"
		tx = tx.Where("email ILIKE ? OR name ILIKE ?", like, like)
	}
	if status != "" {
		tx = tx.Where("status = ?", status)
	}
	if role != "" {
		tx = tx.Where("EXISTS (SELECT 1 FROM user_roles ur JOIN roles r ON r.id = ur.role_id "+
			"WHERE ur.user_id = users.id AND r.slug = ?)", role)
	}

	return tx.Order("created_at DESC")
}

func (r *UserRepository) CountByStatus(db *gorm.DB) (map[string]int64, error) {
	type row struct {
		Status string
		Total  int64
	}
	var rows []row
	if err := db.Model(&entity.User{}).Select("status, COUNT(*) AS total").Group("status").Scan(&rows).Error; err != nil {
		return nil, err
	}

	counts := map[string]int64{}
	for _, item := range rows {
		counts[item.Status] = item.Total
		counts["total"] += item.Total
	}
	return counts, nil
}
