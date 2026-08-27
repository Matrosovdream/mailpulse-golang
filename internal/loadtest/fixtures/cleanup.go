package fixtures

import (
	"context"
	"fmt"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Cleanup removes everything a load run created, and nothing else.
//
// It deletes by the load domain rather than truncating, because these suites
// run against the development database where somebody's own data is sitting in
// the same tables. Order is child-to-parent: the pipeline creates matches,
// runs and deliveries during a run, and those point back at rows this deletes
// last.
func Cleanup(ctx context.Context, db *gorm.DB) error {
	// every statement narrows through users.email, which is the only column a
	// load run controls end to end
	loadUsers := "SELECT id FROM users WHERE email LIKE '%@" + EmailDomain + "'"
	loadAccounts := "SELECT id FROM mail_accounts WHERE user_id IN (" + loadUsers + ")"
	loadWatchers := "SELECT id FROM watchers WHERE user_id IN (" + loadUsers + ")"
	loadEvents := "SELECT id FROM watcher_events WHERE watcher_id IN (" + loadWatchers + ")"

	statements := []string{
		"DELETE FROM notification_deliveries WHERE event_run_id IN (SELECT id FROM event_runs WHERE watcher_event_id IN (" + loadEvents + "))",
		"DELETE FROM event_runs WHERE watcher_event_id IN (" + loadEvents + ")",
		"DELETE FROM watcher_event_notifiers WHERE watcher_event_id IN (" + loadEvents + ")",
		"DELETE FROM watcher_events WHERE watcher_id IN (" + loadWatchers + ")",
		"DELETE FROM watcher_filters WHERE watcher_id IN (" + loadWatchers + ")",
		"DELETE FROM matched_emails WHERE watcher_id IN (" + loadWatchers + ")",
		"DELETE FROM watchers WHERE user_id IN (" + loadUsers + ")",
		"DELETE FROM mail_sync_runs WHERE mail_account_id IN (" + loadAccounts + ")",
		"DELETE FROM mail_accounts WHERE user_id IN (" + loadUsers + ")",
		"DELETE FROM notifiers WHERE user_id IN (" + loadUsers + ")",
		"DELETE FROM audit_logs WHERE actor_user_id IN (" + loadUsers + ") OR impersonated_user_id IN (" + loadUsers + ")",
		"DELETE FROM user_sessions WHERE user_id IN (" + loadUsers + ")",
		"DELETE FROM user_roles WHERE user_id IN (" + loadUsers + ")",
		"DELETE FROM users WHERE email LIKE '%@" + EmailDomain + "'",
	}

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, statement := range statements {
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("%s: %w", statement, err)
			}
		}
		return nil
	})
}

// Count reports how many load-owned rows are currently in the two tables worth
// knowing about before a run starts.
func Count(ctx context.Context, db *gorm.DB) (users int64, accounts int64, err error) {
	if err = db.WithContext(ctx).Raw(
		"SELECT count(*) FROM users WHERE email LIKE '%@" + EmailDomain + "'").Scan(&users).Error; err != nil {
		return 0, 0, err
	}

	err = db.WithContext(ctx).Raw(
		"SELECT count(*) FROM mail_accounts WHERE email_address LIKE '%@" + EmailDomain + "'").Scan(&accounts).Error

	return users, accounts, err
}

// hashOnce is bcrypt at the cheapest cost the library allows. Fixture users
// exist to be counted and polled, not to model password strength, and the
// default cost would put minutes into building a large shape.
func hashOnce(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}
