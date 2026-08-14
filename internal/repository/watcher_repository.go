package repository

import (
	"mailpulse/internal/entity"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type WatcherRepository struct {
	Repository[entity.Watcher]
	Log *logrus.Logger
}

func NewWatcherRepository(log *logrus.Logger) *WatcherRepository {
	return &WatcherRepository{Log: log}
}

// Search treats an empty or "all" status as no filter, which is what the
// dashboard's archive/all toggle sends.
func (r *WatcherRepository) Search(db *gorm.DB, userID, status, mailAccountID, query string) *gorm.DB {
	tx := db.Model(&entity.Watcher{})

	if userID != "" {
		tx = tx.Where("user_id = ?", userID)
	}
	if status != "" && status != "all" {
		tx = tx.Where("status = ?", status)
	}
	if mailAccountID != "" {
		tx = tx.Where("mail_account_id = ?", mailAccountID)
	}
	if query != "" {
		tx = tx.Where("name ILIKE ?", "%"+query+"%")
	}

	return tx.Order("created_at DESC")
}

func (r *WatcherRepository) FindActiveByMailAccount(db *gorm.DB, mailAccountID string) ([]entity.Watcher, error) {
	var watchers []entity.Watcher
	err := db.Where("mail_account_id = ? AND status = ?", mailAccountID, entity.WatcherStatusActive).
		Find(&watchers).Error
	return watchers, err
}

func (r *WatcherRepository) CountByMailAccount(db *gorm.DB, mailAccountID string) (int64, error) {
	var total int64
	err := db.Model(&entity.Watcher{}).Where("mail_account_id = ?", mailAccountID).Count(&total).Error
	return total, err
}

func (r *WatcherRepository) NamesByMailAccount(db *gorm.DB, mailAccountID string) ([]string, error) {
	var names []string
	err := db.Model(&entity.Watcher{}).
		Where("mail_account_id = ?", mailAccountID).
		Pluck("name", &names).Error
	return names, err
}

func (r *WatcherRepository) PauseAllForUser(db *gorm.DB, userID string) (int64, error) {
	tx := db.Model(&entity.Watcher{}).
		Where("user_id = ? AND status = ?", userID, entity.WatcherStatusActive).
		Update("status", entity.WatcherStatusPaused)
	return tx.RowsAffected, tx.Error
}

func (r *WatcherRepository) CountByStatus(db *gorm.DB) (map[string]int64, error) {
	return countByColumn(db, &entity.Watcher{}, "status")
}

func (r *WatcherRepository) CountByStatusForUser(db *gorm.DB, userID string) (map[string]int64, error) {
	return countByColumn(db.Where("user_id = ?", userID), &entity.Watcher{}, "status")
}

func (r *WatcherRepository) CountForUser(db *gorm.DB, userID string) (int64, error) {
	var total int64
	err := db.Model(&entity.Watcher{}).Where("user_id = ?", userID).Count(&total).Error
	return total, err
}

func (r *WatcherRepository) RecordMatch(db *gorm.DB, watcherID string, at int64) error {
	return db.Model(&entity.Watcher{}).Where("id = ?", watcherID).
		Updates(map[string]any{
			"match_count":     gorm.Expr("match_count + 1"),
			"last_matched_at": at,
		}).Error
}

// ---------------------------------------------------------------- filters

type WatcherFilterRepository struct {
	Repository[entity.WatcherFilter]
	Log *logrus.Logger
}

func NewWatcherFilterRepository(log *logrus.Logger) *WatcherFilterRepository {
	return &WatcherFilterRepository{Log: log}
}

func (r *WatcherFilterRepository) FindByWatcher(db *gorm.DB, watcherID string) ([]entity.WatcherFilter, error) {
	var filters []entity.WatcherFilter
	err := db.Where("watcher_id = ?", watcherID).Order("position ASC").Find(&filters).Error
	return filters, err
}

func (r *WatcherFilterRepository) FindByWatchers(db *gorm.DB, watcherIDs []string) (map[string][]entity.WatcherFilter, error) {
	grouped := map[string][]entity.WatcherFilter{}
	if len(watcherIDs) == 0 {
		return grouped, nil
	}

	var filters []entity.WatcherFilter
	if err := db.Where("watcher_id IN ?", watcherIDs).Order("position ASC").Find(&filters).Error; err != nil {
		return nil, err
	}

	for _, filter := range filters {
		grouped[filter.WatcherID] = append(grouped[filter.WatcherID], filter)
	}
	return grouped, nil
}

func (r *WatcherFilterRepository) DeleteByWatcher(db *gorm.DB, watcherID string) error {
	return db.Where("watcher_id = ?", watcherID).Delete(&entity.WatcherFilter{}).Error
}
