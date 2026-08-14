package repository

import (
	"mailpulse/internal/entity"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type EventRunRepository struct {
	Repository[entity.EventRun]
	Log *logrus.Logger
}

func NewEventRunRepository(log *logrus.Logger) *EventRunRepository {
	return &EventRunRepository{Log: log}
}

// ClaimDue is the dispatcher's queue read, riding idx_event_runs_due.
func (r *EventRunRepository) ClaimDue(db *gorm.DB, now int64, limit int) ([]entity.EventRun, error) {
	var runs []entity.EventRun
	err := db.Where("status = ? AND scheduled_at <= ?", entity.RunStatusPending, now).
		Order("scheduled_at ASC").
		Limit(limit).
		Clauses(lockSkipLocked()).
		Find(&runs).Error
	return runs, err
}

func (r *EventRunRepository) Search(db *gorm.DB, req RunSearch) *gorm.DB {
	tx := db.Model(&entity.EventRun{})

	if req.UserID != "" {
		tx = tx.Where("user_id = ?", req.UserID)
	}
	if req.Status != "" {
		tx = tx.Where("status = ?", req.Status)
	}
	if req.WatcherID != "" {
		tx = tx.Where("watcher_event_id IN (SELECT id FROM watcher_events WHERE watcher_id = ?)", req.WatcherID)
	}
	if req.MatchedEmailID != "" {
		tx = tx.Where("matched_email_id = ?", req.MatchedEmailID)
	}
	if req.From > 0 {
		tx = tx.Where("created_at >= ?", req.From)
	}
	if req.To > 0 {
		tx = tx.Where("created_at <= ?", req.To)
	}

	return tx.Order("created_at DESC")
}

type RunSearch struct {
	UserID         string
	WatcherID      string
	MatchedEmailID string
	Status         string
	From           int64
	To             int64
}

func (r *EventRunRepository) FindByMatch(db *gorm.DB, matchedEmailID string) ([]entity.EventRun, error) {
	var runs []entity.EventRun
	err := db.Where("matched_email_id = ?", matchedEmailID).
		Order("occurrence ASC").
		Find(&runs).Error
	return runs, err
}

// CancelPendingAfter stops the remaining occurrences of a repeating event
// without touching runs that already settled.
func (r *EventRunRepository) CancelPendingAfter(db *gorm.DB, matchedEmailID, watcherEventID string, occurrence int) (int64, error) {
	tx := db.Model(&entity.EventRun{}).
		Where("matched_email_id = ? AND watcher_event_id = ? AND occurrence >= ? AND status = ?",
			matchedEmailID, watcherEventID, occurrence, entity.RunStatusPending).
		Update("status", entity.RunStatusCancelled)
	return tx.RowsAffected, tx.Error
}

func (r *EventRunRepository) CountByStatusForWatcher(db *gorm.DB, watcherID string) (map[string]int64, error) {
	return countByColumn(
		db.Where("watcher_event_id IN (SELECT id FROM watcher_events WHERE watcher_id = ?)", watcherID),
		&entity.EventRun{}, "status")
}

func (r *EventRunRepository) CountByStatusSince(db *gorm.DB, since int64) (map[string]int64, error) {
	return countByColumn(db.Where("created_at >= ?", since), &entity.EventRun{}, "status")
}

func (r *EventRunRepository) CountFailedSince(db *gorm.DB, userID string, since int64) (int64, error) {
	tx := db.Model(&entity.EventRun{}).
		Where("status = ? AND created_at >= ?", entity.RunStatusFailed, since)
	if userID != "" {
		tx = tx.Where("user_id = ?", userID)
	}
	var total int64
	err := tx.Count(&total).Error
	return total, err
}

// QueueDepth powers the admin stats card: how much is waiting, how much is late.
func (r *EventRunRepository) QueueDepth(db *gorm.DB, now int64) (pending, overdue int64, oldest int64, err error) {
	if err = db.Model(&entity.EventRun{}).
		Where("status = ?", entity.RunStatusPending).Count(&pending).Error; err != nil {
		return
	}

	if err = db.Model(&entity.EventRun{}).
		Where("status = ? AND scheduled_at <= ?", entity.RunStatusPending, now).
		Count(&overdue).Error; err != nil {
		return
	}

	var oldestScheduled *int64
	if err = db.Model(&entity.EventRun{}).
		Where("status = ?", entity.RunStatusPending).
		Select("MIN(scheduled_at)").
		Scan(&oldestScheduled).Error; err != nil {
		return
	}

	if oldestScheduled != nil && *oldestScheduled < now {
		oldest = (now - *oldestScheduled) / 1000
	}
	return
}

func (r *EventRunRepository) FindRecent(db *gorm.DB, userID string, limit int) ([]entity.EventRun, error) {
	var runs []entity.EventRun
	err := db.Where("user_id = ?", userID).Order("created_at DESC").Limit(limit).Find(&runs).Error
	return runs, err
}

// ---------------------------------------------------------------- deliveries

type NotificationDeliveryRepository struct {
	Repository[entity.NotificationDelivery]
	Log *logrus.Logger
}

func NewNotificationDeliveryRepository(log *logrus.Logger) *NotificationDeliveryRepository {
	return &NotificationDeliveryRepository{Log: log}
}

func (r *NotificationDeliveryRepository) FindByRun(db *gorm.DB, eventRunID string) ([]entity.NotificationDelivery, error) {
	var deliveries []entity.NotificationDelivery
	err := db.Where("event_run_id = ?", eventRunID).Order("created_at ASC").Find(&deliveries).Error
	return deliveries, err
}

func (r *NotificationDeliveryRepository) FindByRuns(db *gorm.DB, runIDs []string) (map[string][]entity.NotificationDelivery, error) {
	grouped := map[string][]entity.NotificationDelivery{}
	if len(runIDs) == 0 {
		return grouped, nil
	}

	var deliveries []entity.NotificationDelivery
	if err := db.Where("event_run_id IN ?", runIDs).Order("created_at ASC").Find(&deliveries).Error; err != nil {
		return nil, err
	}

	for _, delivery := range deliveries {
		grouped[delivery.EventRunID] = append(grouped[delivery.EventRunID], delivery)
	}
	return grouped, nil
}

// Search scopes deliveries to the caller by joining through the parent run,
// since notification_deliveries has no user_id of its own.
func (r *NotificationDeliveryRepository) Search(db *gorm.DB, userID, eventRunID, notifierID, status string) *gorm.DB {
	tx := db.Model(&entity.NotificationDelivery{})

	if userID != "" {
		tx = tx.Where("event_run_id IN (SELECT id FROM event_runs WHERE user_id = ?)", userID)
	}
	if eventRunID != "" {
		tx = tx.Where("event_run_id = ?", eventRunID)
	}
	if notifierID != "" {
		tx = tx.Where("notifier_id = ?", notifierID)
	}
	if status != "" {
		tx = tx.Where("status = ?", status)
	}

	return tx.Order("created_at DESC")
}
