package repository

import (
	"mailpulse/internal/entity"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type AuditLogRepository struct {
	Repository[entity.AuditLog]
	Log *logrus.Logger
}

func NewAuditLogRepository(log *logrus.Logger) *AuditLogRepository {
	return &AuditLogRepository{Log: log}
}

func (r *AuditLogRepository) Search(db *gorm.DB, actorUserID, action, entityType string, from, to int64) *gorm.DB {
	tx := db.Model(&entity.AuditLog{})

	if actorUserID != "" {
		tx = tx.Where("actor_user_id = ?", actorUserID)
	}
	if action != "" {
		tx = tx.Where("action = ?", action)
	}
	if entityType != "" {
		tx = tx.Where("entity_type = ?", entityType)
	}
	if from > 0 {
		tx = tx.Where("created_at >= ?", from)
	}
	if to > 0 {
		tx = tx.Where("created_at <= ?", to)
	}

	return tx.Order("created_at DESC")
}

// EmailsFor resolves actor ids to emails in one query for the admin log table.
func (r *AuditLogRepository) EmailsFor(db *gorm.DB, logs []entity.AuditLog) (map[string]string, error) {
	ids := make([]string, 0, len(logs))
	seen := map[string]bool{}
	for _, item := range logs {
		if item.ActorUserID != nil && !seen[*item.ActorUserID] {
			seen[*item.ActorUserID] = true
			ids = append(ids, *item.ActorUserID)
		}
	}

	emails := map[string]string{}
	if len(ids) == 0 {
		return emails, nil
	}

	type row struct {
		ID    string
		Email string
	}
	var rows []row
	if err := db.Model(&entity.User{}).Select("id, email").Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, err
	}

	for _, item := range rows {
		emails[item.ID] = item.Email
	}
	return emails, nil
}
