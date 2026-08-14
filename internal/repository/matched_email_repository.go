package repository

import (
	"mailpulse/internal/entity"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MatchedEmailRepository struct {
	Repository[entity.MatchedEmail]
	Log *logrus.Logger
}

func NewMatchedEmailRepository(log *logrus.Logger) *MatchedEmailRepository {
	return &MatchedEmailRepository{Log: log}
}

// CreateIfNew leans on the unique (watcher_id, message_id) index instead of a
// read-then-write race: a duplicate is a no-op and reports false, which is what
// stops a re-read mailbox from firing the same watcher twice.
func (r *MatchedEmailRepository) CreateIfNew(db *gorm.DB, match *entity.MatchedEmail) (bool, error) {
	tx := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "watcher_id"}, {Name: "message_id"}},
		DoNothing: true,
	}).Create(match)

	if tx.Error != nil {
		return false, tx.Error
	}
	return tx.RowsAffected > 0, nil
}

func (r *MatchedEmailRepository) Search(db *gorm.DB, req MatchSearch) *gorm.DB {
	tx := db.Model(&entity.MatchedEmail{})

	if req.UserID != "" {
		tx = tx.Where("user_id = ?", req.UserID)
	}
	if req.WatcherID != "" {
		tx = tx.Where("watcher_id = ?", req.WatcherID)
	}
	if req.MailAccountID != "" {
		tx = tx.Where("mail_account_id = ?", req.MailAccountID)
	}
	if req.From > 0 {
		tx = tx.Where("matched_at >= ?", req.From)
	}
	if req.To > 0 {
		tx = tx.Where("matched_at <= ?", req.To)
	}
	if req.Query != "" {
		like := "%" + req.Query + "%"
		tx = tx.Where("subject ILIKE ? OR from_address ILIKE ?", like, like)
	}

	return tx.Order("matched_at DESC")
}

type MatchSearch struct {
	UserID        string
	WatcherID     string
	MailAccountID string
	From          int64
	To            int64
	Query         string
}

func (r *MatchedEmailRepository) CountSince(db *gorm.DB, userID string, since int64) (int64, error) {
	tx := db.Model(&entity.MatchedEmail{}).Where("matched_at >= ?", since)
	if userID != "" {
		tx = tx.Where("user_id = ?", userID)
	}
	var total int64
	err := tx.Count(&total).Error
	return total, err
}

func (r *MatchedEmailRepository) CountForWatcher(db *gorm.DB, watcherID string) (int64, error) {
	var total int64
	err := db.Model(&entity.MatchedEmail{}).Where("watcher_id = ?", watcherID).Count(&total).Error
	return total, err
}

func (r *MatchedEmailRepository) CountForUser(db *gorm.DB, userID string) (int64, error) {
	var total int64
	err := db.Model(&entity.MatchedEmail{}).Where("user_id = ?", userID).Count(&total).Error
	return total, err
}

// DailyCounts buckets by day in UTC for the watcher detail chart.
func (r *MatchedEmailRepository) DailyCounts(db *gorm.DB, watcherID string, since int64) ([]DailyCount, error) {
	var rows []DailyCount
	err := db.Model(&entity.MatchedEmail{}).
		Select("TO_CHAR(TO_TIMESTAMP(matched_at / 1000) AT TIME ZONE 'UTC', 'YYYY-MM-DD') AS date, COUNT(*) AS total").
		Where("watcher_id = ? AND matched_at >= ?", watcherID, since).
		Group("date").
		Order("date ASC").
		Scan(&rows).Error
	return rows, err
}

type DailyCount struct {
	Date  string
	Total int64
}

func (r *MatchedEmailRepository) FindRecent(db *gorm.DB, userID string, limit int) ([]entity.MatchedEmail, error) {
	var matches []entity.MatchedEmail
	err := db.Where("user_id = ?", userID).Order("matched_at DESC").Limit(limit).Find(&matches).Error
	return matches, err
}
