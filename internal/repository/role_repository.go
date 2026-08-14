package repository

import (
	"mailpulse/internal/entity"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type RoleRepository struct {
	Repository[entity.Role]
	Log *logrus.Logger
}

func NewRoleRepository(log *logrus.Logger) *RoleRepository {
	return &RoleRepository{Log: log}
}

func (r *RoleRepository) FindBySlug(db *gorm.DB, role *entity.Role, slug string) error {
	return db.Where("slug = ?", slug).Take(role).Error
}

func (r *RoleRepository) FindBySlugs(db *gorm.DB, slugs []string) ([]entity.Role, error) {
	var roles []entity.Role
	if len(slugs) == 0 {
		return roles, nil
	}
	err := db.Where("slug IN ?", slugs).Find(&roles).Error
	return roles, err
}

func (r *RoleRepository) FindAll(db *gorm.DB) ([]entity.Role, error) {
	var roles []entity.Role
	err := db.Order("slug ASC").Find(&roles).Error
	return roles, err
}
