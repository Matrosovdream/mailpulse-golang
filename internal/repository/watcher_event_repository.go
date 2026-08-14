package repository

import (
	"mailpulse/internal/entity"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type WatcherEventRepository struct {
	Repository[entity.WatcherEvent]
	Log *logrus.Logger
}

func NewWatcherEventRepository(log *logrus.Logger) *WatcherEventRepository {
	return &WatcherEventRepository{Log: log}
}

func (r *WatcherEventRepository) FindByWatcher(db *gorm.DB, watcherID string) ([]entity.WatcherEvent, error) {
	var events []entity.WatcherEvent
	err := db.Where("watcher_id = ?", watcherID).Order("position ASC").Find(&events).Error
	return events, err
}

func (r *WatcherEventRepository) FindEnabledByWatcher(db *gorm.DB, watcherID string) ([]entity.WatcherEvent, error) {
	var events []entity.WatcherEvent
	err := db.Where("watcher_id = ? AND enabled = true", watcherID).Order("position ASC").Find(&events).Error
	return events, err
}

func (r *WatcherEventRepository) FindByIdAndWatcher(db *gorm.DB, event *entity.WatcherEvent, id, watcherID string) error {
	return db.Where("id = ? AND watcher_id = ?", id, watcherID).Take(event).Error
}

func (r *WatcherEventRepository) UpdatePosition(db *gorm.DB, id string, position int) error {
	return db.Model(&entity.WatcherEvent{}).Where("id = ?", id).Update("position", position).Error
}

// ReplaceNotifiers rewrites the join rows for one event.
func (r *WatcherEventRepository) ReplaceNotifiers(db *gorm.DB, eventID string, notifierIDs []string) error {
	if err := db.Where("watcher_event_id = ?", eventID).Delete(&entity.WatcherEventNotifier{}).Error; err != nil {
		return err
	}

	if len(notifierIDs) == 0 {
		return nil
	}

	links := make([]entity.WatcherEventNotifier, 0, len(notifierIDs))
	for i, notifierID := range notifierIDs {
		links = append(links, entity.WatcherEventNotifier{
			WatcherEventID: eventID,
			NotifierID:     notifierID,
			Position:       i,
		})
	}

	return db.Create(&links).Error
}

func (r *WatcherEventRepository) FindNotifiers(db *gorm.DB, eventID string) ([]entity.Notifier, error) {
	var notifiers []entity.Notifier
	err := db.Model(&entity.Notifier{}).
		Joins("JOIN watcher_event_notifiers wen ON wen.notifier_id = notifiers.id").
		Where("wen.watcher_event_id = ?", eventID).
		Order("wen.position ASC").
		Find(&notifiers).Error
	return notifiers, err
}

func (r *WatcherEventRepository) FindNotifiersForEvents(db *gorm.DB, eventIDs []string) (map[string][]entity.Notifier, error) {
	grouped := map[string][]entity.Notifier{}
	if len(eventIDs) == 0 {
		return grouped, nil
	}

	type row struct {
		WatcherEventID string
		entity.Notifier
	}

	var rows []row
	err := db.Table("notifiers").
		Select("wen.watcher_event_id AS watcher_event_id, notifiers.*").
		Joins("JOIN watcher_event_notifiers wen ON wen.notifier_id = notifiers.id").
		Where("wen.watcher_event_id IN ?", eventIDs).
		Order("wen.position ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	for _, item := range rows {
		grouped[item.WatcherEventID] = append(grouped[item.WatcherEventID], item.Notifier)
	}
	return grouped, nil
}

func (r *WatcherEventRepository) DeleteByWatcher(db *gorm.DB, watcherID string) error {
	return db.Where("watcher_id = ?", watcherID).Delete(&entity.WatcherEvent{}).Error
}
