//go:build feature

package feature

import (
	"context"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"mailpulse/test/support"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sendMail delivers a real message to the test mail server over SMTP, so the
// pipeline is exercised against mail it did not invent.
func sendMail(t *testing.T, h *support.Harness, subject, from, body string) {
	t.Helper()

	host := h.Config.GetString("test.mail_host")
	port := h.Config.GetInt("test.smtp_port")
	to := h.Config.GetString("test.mail_address")

	message := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMessage-ID: <%d@feature.test>\r\n\r\n%s\r\n",
		from, to, subject, time.Now().UnixNano(), body)

	address := fmt.Sprintf("%s:%d", host, port)
	if err := smtp.SendMail(address, nil, from, []string{to}, []byte(message)); err != nil {
		t.Fatalf("could not deliver test mail to %s: %v", address, err)
	}
}

// TestPipelineEndToEnd walks the whole product: real mail arrives, a watcher
// matches it, the event is scheduled, the dispatcher runs it, and a delivery is
// recorded — all observed through the API a SPA would use.
func TestPipelineEndToEnd(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	account := h.Register(t, "pipeline@example.com", "secret123")
	accountID := connectedAccount(t, h, account.Token, "pipeline@corp.com")
	notifierID := verifiedNotifier(t, h, account.Token, "Pipeline hook")

	marker := fmt.Sprintf("pipeline-%d", time.Now().UnixNano())
	sendMail(t, h, "A new user "+marker, "notifications@stripe.com", "Someone signed up.")
	sendMail(t, h, "Unrelated newsletter", "news@letter.example", "Nothing to see here.")

	created := h.Post(t, "/api/watchers", account.Token, map[string]any{
		"mail_account_id": accountID,
		"name":            "Pipeline watcher",
		"match_mode":      "all",
		"watch_from":      0,
		"filters": []map[string]any{
			{"field": "subject", "operator": "contains", "value": marker},
		},
		"events": []map[string]any{
			{"type": "notify", "notifier_ids": []string{notifierID}},
		},
	})
	require.Equal(t, http.StatusCreated, created.Status)

	var watcher struct {
		ID string `json:"id"`
	}
	created.Decode(t, &watcher)

	// ---- sync reads the mailbox and records exactly the matching message
	synced := h.Post(t, "/api/mail-accounts/"+accountID+"/_sync", account.Token, nil)
	require.Equal(t, http.StatusOK, synced.Status)

	var sync struct {
		MessagesFetched int `json:"messages_fetched"`
		MatchesCreated  int `json:"matches_created"`
	}
	synced.Decode(t, &sync)

	assert.GreaterOrEqual(t, sync.MessagesFetched, 2, "both messages should have been read")
	require.Equal(t, 1, sync.MatchesCreated, "only the marked subject matches")

	// ---- the match records why it fired
	matches := h.Get(t, "/api/matches", account.Token)
	var recorded []struct {
		ID             string   `json:"id"`
		Subject        string   `json:"subject"`
		MatchedFilters []string `json:"matched_filters"`
	}
	matches.Decode(t, &recorded)

	require.Len(t, recorded, 1)
	assert.Contains(t, recorded[0].Subject, marker)
	require.NotEmpty(t, recorded[0].MatchedFilters)
	assert.Contains(t, recorded[0].MatchedFilters[0], "subject contains")

	// ---- syncing again must not re-fire: the dedupe index is the guard
	resynced := h.Post(t, "/api/mail-accounts/"+accountID+"/_sync", account.Token, nil)
	require.Equal(t, http.StatusOK, resynced.Status)
	resynced.Decode(t, &sync)
	assert.Equal(t, 0, sync.MatchesCreated, "the same message must not match twice")

	// ---- the event was scheduled
	runs := h.Get(t, "/api/event-runs", account.Token)
	var scheduled []struct {
		ID         string `json:"id"`
		Occurrence int    `json:"occurrence"`
		Status     string `json:"status"`
	}
	runs.Decode(t, &scheduled)

	require.Len(t, scheduled, 1)
	assert.Equal(t, 1, scheduled[0].Occurrence)
	assert.Equal(t, "pending", scheduled[0].Status)

	// ---- the dispatcher executes it. The notifier points at an unreachable
	// port on purpose: the delivery attempt and its failure are what is being
	// verified, not a working webhook.
	handled, err := h.Container.Dispatcher.Tick(context.Background(), 10)
	require.NoError(t, err)
	assert.Equal(t, 1, handled)

	detail := h.Get(t, "/api/event-runs/"+scheduled[0].ID, account.Token)
	var run struct {
		Status     string  `json:"status"`
		Attempt    int     `json:"attempt"`
		Error      *string `json:"error"`
		Deliveries []struct {
			ChannelType     string  `json:"channel_type"`
			Status          string  `json:"status"`
			RenderedMessage *string `json:"rendered_message"`
			Error           *string `json:"error"`
		} `json:"deliveries"`
	}
	detail.Decode(t, &run)

	assert.Equal(t, 1, run.Attempt, "the attempt counter must advance on every execution")
	require.Len(t, run.Deliveries, 1, "one delivery per attached notifier")
	assert.Equal(t, "webhook", run.Deliveries[0].ChannelType)
	assert.Equal(t, "failed", run.Deliveries[0].Status)
	require.NotNil(t, run.Deliveries[0].RenderedMessage)
	assert.Contains(t, *run.Deliveries[0].RenderedMessage, "Pipeline watcher",
		"the rendered message is what support reads back")

	// ---- the failure is retried rather than abandoned
	assert.Equal(t, "pending", run.Status)
	require.NotNil(t, run.Error)

	// ---- the dashboard reflects all of it
	summary := h.Get(t, "/api/dashboard/summary", account.Token)
	var dashboard struct {
		Watchers   map[string]int64 `json:"watchers"`
		Matches24h int64            `json:"matches_24h"`
		Recent     []struct {
			Type string `json:"type"`
		} `json:"recent"`
	}
	summary.Decode(t, &dashboard)

	assert.Equal(t, int64(1), dashboard.Watchers["active"])
	assert.Equal(t, int64(1), dashboard.Matches24h)
	require.NotEmpty(t, dashboard.Recent)
	assert.Equal(t, "match", dashboard.Recent[0].Type)
}

// A paused watcher must stop producing matches without being deleted.
func TestPausedWatcherDoesNotMatch(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	account := h.Register(t, "paused@example.com", "secret123")
	accountID := connectedAccount(t, h, account.Token, "paused@corp.com")

	marker := fmt.Sprintf("paused-%d", time.Now().UnixNano())

	created := h.Post(t, "/api/watchers", account.Token, map[string]any{
		"mail_account_id": accountID, "name": "Paused watcher", "watch_from": 0,
		"filters": []map[string]any{
			{"field": "subject", "operator": "contains", "value": marker},
		},
	})
	require.Equal(t, http.StatusCreated, created.Status)

	var watcher struct {
		ID string `json:"id"`
	}
	created.Decode(t, &watcher)

	require.Equal(t, http.StatusOK,
		h.Post(t, "/api/watchers/"+watcher.ID+"/_pause", account.Token, nil).Status)

	sendMail(t, h, "Message "+marker, "someone@example.com", "body")

	synced := h.Post(t, "/api/mail-accounts/"+accountID+"/_sync", account.Token, nil)
	require.Equal(t, http.StatusOK, synced.Status)

	var sync struct {
		MatchesCreated int `json:"matches_created"`
	}
	synced.Decode(t, &sync)
	assert.Equal(t, 0, sync.MatchesCreated)
}

// watch_from is what stops a new watcher firing on a year of history.
func TestWatchFromExcludesOlderMail(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	account := h.Register(t, "watchfrom@example.com", "secret123")
	accountID := connectedAccount(t, h, account.Token, "watchfrom@corp.com")

	marker := fmt.Sprintf("history-%d", time.Now().UnixNano())
	sendMail(t, h, "Old message "+marker, "someone@example.com", "body")

	// only mail arriving from now on counts
	future := time.Now().Add(time.Hour).UnixMilli()

	created := h.Post(t, "/api/watchers", account.Token, map[string]any{
		"mail_account_id": accountID, "name": "Future only", "watch_from": future,
		"filters": []map[string]any{
			{"field": "subject", "operator": "contains", "value": marker},
		},
	})
	require.Equal(t, http.StatusCreated, created.Status)

	synced := h.Post(t, "/api/mail-accounts/"+accountID+"/_sync", account.Token, nil)
	require.Equal(t, http.StatusOK, synced.Status)

	var sync struct {
		MatchesCreated int `json:"matches_created"`
	}
	synced.Decode(t, &sync)
	assert.Equal(t, 0, sync.MatchesCreated,
		"mail older than watch_from must be ignored, "+strings.Repeat("", 0))
}
