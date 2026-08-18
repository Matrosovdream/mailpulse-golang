package repository

import (
	"mailpulse/internal/entity"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type MailAccountRepository struct {
	Repository[entity.MailAccount]
	Log *logrus.Logger
}

func NewMailAccountRepository(log *logrus.Logger) *MailAccountRepository {
	return &MailAccountRepository{Log: log}
}

func (r *MailAccountRepository) Search(db *gorm.DB, userID, status, provider string) *gorm.DB {
	tx := db.Model(&entity.MailAccount{})

	if userID != "" {
		tx = tx.Where("user_id = ?", userID)
	}
	if status != "" {
		tx = tx.Where("status = ?", status)
	}
	if provider != "" {
		tx = tx.Where("provider = ?", provider)
	}

	return tx.Order("created_at DESC")
}

func (r *MailAccountRepository) CountByEmail(db *gorm.DB, userID, email string) (int64, error) {
	var total int64
	err := db.Model(&entity.MailAccount{}).
		Where("user_id = ? AND email_address = ?", userID, email).
		Count(&total).Error
	return total, err
}

// FindDue claims the accounts whose poll has come around. SKIP LOCKED lets
// several workers share the queue without handing the same mailbox to two.
func (r *MailAccountRepository) FindDue(db *gorm.DB, now int64, limit int) ([]entity.MailAccount, error) {
	var accounts []entity.MailAccount
	err := db.Where("status = ? AND next_poll_at <= ?", entity.MailAccountStatusVerified, now).
		Order("next_poll_at ASC").
		Limit(limit).
		Clauses(lockSkipLocked()).
		Find(&accounts).Error
	return accounts, err
}

// FindStale returns accounts whose credentials have not been checked since
// `before`. Accounts already in error are included so a mailbox that comes back
// (password fixed at the provider, server outage over) recovers on its own
// instead of staying dead until someone notices.
func (r *MailAccountRepository) FindStale(db *gorm.DB, before int64, limit int) ([]entity.MailAccount, error) {
	var accounts []entity.MailAccount
	err := db.Where("status <> ? AND (last_verified_at IS NULL OR last_verified_at < ?)",
		entity.MailAccountStatusDisabled, before).
		Order("last_verified_at ASC NULLS FIRST").
		Limit(limit).
		Clauses(lockSkipLocked()).
		Find(&accounts).Error
	return accounts, err
}

func (r *MailAccountRepository) CountByStatus(db *gorm.DB) (map[string]int64, error) {
	return countByColumn(db, &entity.MailAccount{}, "status")
}

func (r *MailAccountRepository) CountForUser(db *gorm.DB, userID string) (int64, error) {
	var total int64
	err := db.Model(&entity.MailAccount{}).Where("user_id = ?", userID).Count(&total).Error
	return total, err
}
