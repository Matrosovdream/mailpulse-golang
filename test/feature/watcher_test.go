//go:build feature

package feature

import (
	"net/http"
	"testing"

	"mailpulse/test/support"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// connectedAccount returns a verified mailbox pointed at the test mail server.
func connectedAccount(t *testing.T, h *support.Harness, token, address string) string {
	t.Helper()

	host, port := h.MailHost()

	created := h.Post(t, "/api/mail-accounts", token, map[string]any{
		"provider": "imap", "email_address": address, "auth_mode": "password",
		"username": h.Config.GetString("test.mail_username"),
		"password": h.Config.GetString("test.mail_password"),
		"settings": support.MailAccountSettings(host, port),
	})
	require.Equal(t, http.StatusCreated, created.Status)

	var account struct {
		ID string `json:"id"`
	}
	created.Decode(t, &account)

	require.Equal(t, http.StatusOK,
		h.Post(t, "/api/mail-accounts/"+account.ID+"/_verify", token, nil).Status)

	return account.ID
}

// verifiedNotifier returns a webhook notifier, which needs no ownership check.
func verifiedNotifier(t *testing.T, h *support.Harness, token, name string) string {
	t.Helper()

	created := h.Post(t, "/api/notifiers", token, map[string]any{
		"type": "webhook", "name": name,
		"config": map[string]any{"url": "http://127.0.0.1:1/unreachable", "method": "POST"},
	})
	require.Equal(t, http.StatusCreated, created.Status)

	var notifier struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	created.Decode(t, &notifier)
	assert.Equal(t, "verified", notifier.Status)

	return notifier.ID
}

func TestCreateWatcherWithFiltersAndEvents(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	account := h.Register(t, "watchers@example.com", "secret123")

	accountID := connectedAccount(t, h, account.Token, "watch@corp.com")
	notifierID := verifiedNotifier(t, h, account.Token, "Hook")

	// the whole watcher is built in one request, which is what the wizard sends
	created := h.Post(t, "/api/watchers", account.Token, map[string]any{
		"mail_account_id": accountID,
		"name":            "Signup alerts",
		"match_mode":      "all",
		"watch_from":      0,
		"filters": []map[string]any{
			{"field": "subject", "operator": "contains", "value": "signed up"},
		},
		"events": []map[string]any{
			{
				"type": "notify", "run_mode": "recurring",
				"repeat_interval_seconds": 60, "repeat_max": 3,
				"notifier_ids": []string{notifierID},
			},
		},
	})
	require.Equal(t, http.StatusCreated, created.Status)

	var watcher struct {
		ID      string `json:"id"`
		Status  string `json:"status"`
		Filters []struct {
			Field string `json:"field"`
		} `json:"filters"`
		Events []struct {
			Type      string `json:"type"`
			RunMode   string `json:"run_mode"`
			Notifiers []struct {
				ID string `json:"id"`
			} `json:"notifiers"`
		} `json:"events"`
	}
	created.Decode(t, &watcher)

	assert.Equal(t, "active", watcher.Status)
	require.Len(t, watcher.Filters, 1)
	require.Len(t, watcher.Events, 1)
	assert.Equal(t, "recurring", watcher.Events[0].RunMode)
	require.Len(t, watcher.Events[0].Notifiers, 1)
	assert.Equal(t, notifierID, watcher.Events[0].Notifiers[0].ID)
}

func TestWatcherValidation(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	account := h.Register(t, "validation@example.com", "secret123")
	accountID := connectedAccount(t, h, account.Token, "validate@corp.com")

	t.Run("mail account must exist and belong to the caller", func(t *testing.T) {
		response := h.Post(t, "/api/watchers", account.Token, map[string]any{
			"mail_account_id": "00000000-0000-0000-0000-000000000000", "name": "Orphan",
		})
		assert.Equal(t, http.StatusBadRequest, response.Status)
	})

	t.Run("a header filter needs a header name", func(t *testing.T) {
		response := h.Post(t, "/api/watchers", account.Token, map[string]any{
			"mail_account_id": accountID, "name": "Bad filter",
			"filters": []map[string]any{
				{"field": "header", "operator": "contains", "value": "x"},
			},
		})
		assert.Equal(t, http.StatusBadRequest, response.Status)
	})

	t.Run("a recurring event needs a cadence", func(t *testing.T) {
		response := h.Post(t, "/api/watchers", account.Token, map[string]any{
			"mail_account_id": accountID, "name": "No cadence",
			"events": []map[string]any{{"type": "notify", "run_mode": "recurring"}},
		})
		assert.Equal(t, http.StatusBadRequest, response.Status)
	})

	t.Run("a notify event needs a notifier", func(t *testing.T) {
		response := h.Post(t, "/api/watchers", account.Token, map[string]any{
			"mail_account_id": accountID, "name": "No notifier",
			"events": []map[string]any{{"type": "notify"}},
		})
		assert.Equal(t, http.StatusBadRequest, response.Status)
	})

	t.Run("an unknown event type is refused", func(t *testing.T) {
		response := h.Post(t, "/api/watchers", account.Token, map[string]any{
			"mail_account_id": accountID, "name": "Nonsense",
			"events": []map[string]any{{"type": "teleport"}},
		})
		assert.Equal(t, http.StatusBadRequest, response.Status)
	})
}

// The dashboard's archive/all menu is a status filter, so the list endpoint has
// to honour it exactly.
func TestWatcherStatusLifecycle(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	account := h.Register(t, "lifecycle@example.com", "secret123")
	accountID := connectedAccount(t, h, account.Token, "lifecycle@corp.com")

	created := h.Post(t, "/api/watchers", account.Token, map[string]any{
		"mail_account_id": accountID, "name": "Lifecycle",
	})
	require.Equal(t, http.StatusCreated, created.Status)

	var watcher struct {
		ID string `json:"id"`
	}
	created.Decode(t, &watcher)

	statusAfter := func(t *testing.T, action string) string {
		t.Helper()
		response := h.Post(t, "/api/watchers/"+watcher.ID+"/"+action, account.Token, nil)
		require.Equal(t, http.StatusOK, response.Status)

		var updated struct {
			Status string `json:"status"`
		}
		response.Decode(t, &updated)
		return updated.Status
	}

	assert.Equal(t, "paused", statusAfter(t, "_pause"))
	assert.Equal(t, "active", statusAfter(t, "_resume"))
	assert.Equal(t, "archived", statusAfter(t, "_archive"))

	// restoring returns it paused, so it cannot start firing before its owner
	// has looked at it
	assert.Equal(t, "paused", statusAfter(t, "_restore"))

	count := func(t *testing.T, query string) int {
		t.Helper()
		response := h.Get(t, "/api/watchers"+query, account.Token)
		require.Equal(t, http.StatusOK, response.Status)

		var watchers []struct {
			ID string `json:"id"`
		}
		response.Decode(t, &watchers)
		return len(watchers)
	}

	require.Equal(t, http.StatusOK, h.Post(t, "/api/watchers/"+watcher.ID+"/_archive", account.Token, nil).Status)

	assert.Equal(t, 1, count(t, "?status=archived"))
	assert.Equal(t, 0, count(t, "?status=active"))
	assert.Equal(t, 1, count(t, "?status=all"))
}

func TestReplaceFilters(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	account := h.Register(t, "filters@example.com", "secret123")
	accountID := connectedAccount(t, h, account.Token, "filters@corp.com")

	created := h.Post(t, "/api/watchers", account.Token, map[string]any{
		"mail_account_id": accountID, "name": "Filters", "match_mode": "all",
		"filters": []map[string]any{
			{"field": "subject", "operator": "contains", "value": "one"},
		},
	})
	require.Equal(t, http.StatusCreated, created.Status)

	var watcher struct {
		ID string `json:"id"`
	}
	created.Decode(t, &watcher)

	// the UI edits filters as one form, so the whole set is replaced
	replaced := h.Put(t, "/api/watchers/"+watcher.ID+"/filters", account.Token, map[string]any{
		"match_mode": "any",
		"filters": []map[string]any{
			{"field": "subject", "operator": "contains", "value": "two"},
			{"field": "from", "operator": "ends_with", "value": "@corp.com"},
		},
	})
	require.Equal(t, http.StatusOK, replaced.Status)

	var filters []struct {
		Field string `json:"field"`
		Value string `json:"value"`
	}
	replaced.Decode(t, &filters)

	require.Len(t, filters, 2, "the old filter must be gone, not appended to")
	assert.Equal(t, "two", filters[0].Value)

	detail := h.Get(t, "/api/watchers/"+watcher.ID, account.Token)
	var updated struct {
		MatchMode string `json:"match_mode"`
	}
	detail.Decode(t, &updated)
	assert.Equal(t, "any", updated.MatchMode)
}

// The dry run is what turns writing filters from guesswork into feedback, so it
// must read real mail and send nothing.
func TestWatcherDryRun(t *testing.T) {
	h := support.New(t)
	h.Reset(t)
	account := h.Register(t, "dryrun@example.com", "secret123")
	accountID := connectedAccount(t, h, account.Token, "dryrun@corp.com")

	created := h.Post(t, "/api/watchers", account.Token, map[string]any{
		"mail_account_id": accountID, "name": "Dry run", "watch_from": 0,
		"filters": []map[string]any{
			{"field": "subject", "operator": "contains", "value": "zzz-never-matches-zzz"},
		},
	})
	require.Equal(t, http.StatusCreated, created.Status)

	var watcher struct {
		ID string `json:"id"`
	}
	created.Decode(t, &watcher)

	response := h.Post(t, "/api/watchers/"+watcher.ID+"/_test", account.Token,
		map[string]any{"sample_size": 10})
	require.Equal(t, http.StatusOK, response.Status)

	var result struct {
		Scanned int `json:"scanned"`
		Matched int `json:"matched"`
		Samples []struct {
			Subject string `json:"subject"`
			Matched bool   `json:"matched"`
		} `json:"samples"`
	}
	response.Decode(t, &result)

	assert.Equal(t, 0, result.Matched, "the filter was chosen not to match anything")
	assert.Len(t, result.Samples, result.Scanned)

	// nothing was recorded: a dry run must not create matches
	matches := h.Get(t, "/api/matches", account.Token)
	var recorded []struct {
		ID string `json:"id"`
	}
	matches.Decode(t, &recorded)
	assert.Empty(t, recorded)
}
