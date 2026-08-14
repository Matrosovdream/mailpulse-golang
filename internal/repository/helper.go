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
