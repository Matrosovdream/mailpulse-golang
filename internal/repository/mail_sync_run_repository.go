package repository

import (
	"mailpulse/internal/entity"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type MailSyncRunRepository struct {
	Repository[entity.MailSyncRun]
	Log *logrus.Logger
}

func NewMailSyncRunRepository(log *logrus.Logger) *MailSyncRunRepository {
	return &MailSyncRunRepository{Log: log}
}

func (r *MailSyncRunRepository) Search(db *gorm.DB, mailAccountID, status string) *gorm.DB {
	tx := db.Model(&entity.MailSyncRun{})

	if mailAccountID != "" {
		tx = tx.Where("mail_account_id = ?", mailAccountID)
	}
	if status != "" {
		tx = tx.Where("status = ?", status)
	}

	return tx.Order("started_at DESC")
}
