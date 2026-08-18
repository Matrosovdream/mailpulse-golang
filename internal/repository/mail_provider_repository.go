package repository

import (
	"mailpulse/internal/entity"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type MailProviderRepository struct {
	Repository[entity.MailProvider]
	Log *logrus.Logger
}

func NewMailProviderRepository(log *logrus.Logger) *MailProviderRepository {
	return &MailProviderRepository{Log: log}
}

func (r *MailProviderRepository) FindBySlug(db *gorm.DB, provider *entity.MailProvider, slug string) error {
	return db.Where("slug = ?", slug).Take(provider).Error
}

// FindEnabled backs the connect form: only providers a user may pick.
func (r *MailProviderRepository) FindEnabled(db *gorm.DB) ([]entity.MailProvider, error) {
	var providers []entity.MailProvider
	err := db.Where("enabled = true").Order("position ASC").Find(&providers).Error
	return providers, err
}

func (r *MailProviderRepository) FindAll(db *gorm.DB) ([]entity.MailProvider, error) {
	var providers []entity.MailProvider
	err := db.Order("position ASC").Find(&providers).Error
	return providers, err
}
