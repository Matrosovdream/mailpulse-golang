package repository

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// lockSkipLocked is the queue claim used by the mail poller and the event
// dispatcher: rows another worker already holds are skipped rather than waited
// on, so several workers drain one table without blocking each other.
func lockSkipLocked() clause.Locking {
	return clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}
}

// countByColumn returns a status-style breakdown plus a "total" key, which is
// the shape the dashboard and admin stats endpoints render.
func countByColumn(db *gorm.DB, model any, column string) (map[string]int64, error) {
	type row struct {
		Value string
		Total int64
	}

	var rows []row
	err := db.Model(model).
		Select(column + " AS value, COUNT(*) AS total").
		Group(column).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	counts := map[string]int64{}
	for _, item := range rows {
		counts[item.Value] = item.Total
		counts["total"] += item.Total
	}
	return counts, nil
}

// countByUsers counts rows per user_id for a whole page of users in one query.
//
// The alternative is a count per user inside the row loop, which is how the
// admin user list came to run four hundred queries for a single page. A user
// with no rows is simply absent from the map, and a missing key reads as zero,
// which is the answer wanted.
func countByUsers(db *gorm.DB, model any, userIDs []string) (map[string]int64, error) {
	counts := map[string]int64{}
	if len(userIDs) == 0 {
		return counts, nil
	}

	type row struct {
		UserID string
		Total  int64
	}

	var rows []row
	err := db.Model(model).
		Select("user_id, COUNT(*) AS total").
		Where("user_id IN ?", userIDs).
		Group("user_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	for _, item := range rows {
		counts[item.UserID] = item.Total
	}
	return counts, nil
}
