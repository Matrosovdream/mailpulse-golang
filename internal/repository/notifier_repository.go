package repository

import (
	"mailpulse/internal/entity"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type NotifierRepository struct {
	Repository[entity.Notifier]
	Log *logrus.Logger
}

func NewNotifierRepository(log *logrus.Logger) *NotifierRepository {
	return &NotifierRepository{Log: log}
}

func (r *NotifierRepository) Search(db *gorm.DB, userID, notifierType, status string) *gorm.DB {
	tx := db.Model(&entity.Notifier{})

	if userID != "" {
		tx = tx.Where("user_id = ?", userID)
	}
	if notifierType != "" {
		tx = tx.Where("type = ?", notifierType)
	}
	if status != "" {
		tx = tx.Where("status = ?", status)
	}

	return tx.Order("created_at DESC")
}

func (r *NotifierRepository) FindByIds(db *gorm.DB, userID string, ids []string) ([]entity.Notifier, error) {
	var notifiers []entity.Notifier
	if len(ids) == 0 {
		return notifiers, nil
	}
	err := db.Where("user_id = ? AND id IN ?", userID, ids).Find(&notifiers).Error
	return notifiers, err
}

func (r *NotifierRepository) CountByName(db *gorm.DB, userID, name, excludeID string) (int64, error) {
	tx := db.Model(&entity.Notifier{}).Where("user_id = ? AND name = ?", userID, name)
	if excludeID != "" {
		tx = tx.Where("id <> ?", excludeID)
	}
	var total int64
	err := tx.Count(&total).Error
	return total, err
}

// UsedByEvents backs the 409 on delete: the UI needs to name what is blocking.
func (r *NotifierRepository) UsedByEvents(db *gorm.DB, notifierID string) ([]string, error) {
	var names []string
	err := db.Table("watcher_event_notifiers wen").
		Select("DISTINCT w.name").
		Joins("JOIN watcher_events we ON we.id = wen.watcher_event_id").
		Joins("JOIN watchers w ON w.id = we.watcher_id").
		Where("wen.notifier_id = ?", notifierID).
		Scan(&names).Error
	return names, err
}

func (r *NotifierRepository) ClearDefault(db *gorm.DB, userID, exceptID string) error {
	return db.Model(&entity.Notifier{}).
		Where("user_id = ? AND id <> ? AND is_default = true", userID, exceptID).
		Update("is_default", false).Error
}

func (r *NotifierRepository) FindByTypeAndConfig(db *gorm.DB, notifier *entity.Notifier, notifierType, jsonPath, value string) error {
	return db.Where("type = ? AND config ->> ? = ?", notifierType, jsonPath, value).Take(notifier).Error
}

func (r *NotifierRepository) FindPendingByCode(db *gorm.DB, notifier *entity.Notifier, code string, now int64) error {
	return db.Where("verification_code = ? AND status = ? AND verification_expires_at > ?",
		code, entity.NotifierStatusPending, now).Take(notifier).Error
}

func (r *NotifierRepository) CountByStatus(db *gorm.DB) (map[string]int64, error) {
	return countByColumn(db, &entity.Notifier{}, "status")
}

func (r *NotifierRepository) CountForUser(db *gorm.DB, userID string) (int64, error) {
	var total int64
	err := db.Model(&entity.Notifier{}).Where("user_id = ?", userID).Count(&total).Error
	return total, err
}
