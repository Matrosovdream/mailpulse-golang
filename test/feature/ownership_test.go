//go:build feature

package feature

import (
	"net/http"
	"testing"
	"time"

	"mailpulse/internal/entity"
	"mailpulse/test/support"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tenant isolation is enforced per usecase, by hand, roughly forty times:
// c.find(db, id, userID), ownedWatcher(...), FindByIdAndUser(...). Nothing
// structural stops the forty-first route from forgetting it, so the guard is a
// convention — and a convention that is not tested is a convention until
// someone is in a hurry.
//
// This walks every route that takes an id belonging to a user and asserts a
// stranger gets the same answer as if the row did not exist.
//
// 404 and never 403 is the assertion that matters. A 403 confirms the id is
// real, which turns any of these routes into an oracle for enumerating other
// people's mailboxes, watchers and notifiers.

// owned is the set of ids one user owns, so the table below can address every
// shape of route without each entry building its own fixtures.
type owned struct {
	account  string
	watcher  string
	notifier string
	event    string
	match    string
	run      string
	session  string
}

// buildOwned creates one of everything through the API, and drops the two rows
// that only the pipeline would otherwise produce straight into the database.
//
// Inserting those two directly is deliberate. Getting a real match requires
// delivering mail and running a sync, which is covered in pipeline_test.go;
// what is under test here is the ownership guard in front of the row, and that
// guard cannot tell how the row arrived.
func buildOwned(t *testing.T, h *support.Harness, user support.Account) owned {
	t.Helper()

	account := connectedAccount(t, h, user.Token, "owner-isolation@example.com")
	notifier := verifiedNotifier(t, h, user.Token, "owner notifier")

	created := h.Post(t, "/api/watchers", user.Token, map[string]any{
		"mail_account_id": account, "name": "Owned watcher",
		"filters": []map[string]any{
			{"field": "subject", "operator": "contains", "value": "invoice"},
		},
		"events": []map[string]any{
			{"type": "notify", "run_mode": "immediate",
				"config":       map[string]any{"message": "hi"},
				"notifier_ids": []string{notifier}},
		},
	})
	require.Equal(t, http.StatusCreated, created.Status, created.Error(t))

	var watcher struct {
		ID     string `json:"id"`
		Events []struct {
			ID string `json:"id"`
		} `json:"events"`
	}
	created.Decode(t, &watcher)
	require.NotEmpty(t, watcher.Events, "the fixture watcher has no event to address")

	match := &entity.MatchedEmail{
		ID:             uuid.NewString(),
		UserID:         user.ID,
		WatcherID:      watcher.ID,
		MailAccountID:  account,
		MessageID:      "<isolation-" + uuid.NewString() + "@example.com>",
		ProviderUID:    "1",
		Subject:        ptrTo("Private invoice"),
		FromAddress:    ptrTo("sender@example.com"),
		Snippet:        ptrTo("the body of somebody else's mail"),
		ReceivedAt:     time.Now().UnixMilli(),
		MatchedAt:      time.Now().UnixMilli(),
		MatchedFilters: entity.JSON("[]"),
	}
	require.NoError(t, h.DB.Create(match).Error)

	run := &entity.EventRun{
		ID:             uuid.NewString(),
		UserID:         user.ID,
		MatchedEmailID: match.ID,
		WatcherEventID: watcher.Events[0].ID,
		Occurrence:     1,
		Status:         entity.RunStatusPending,
		ScheduledAt:    time.Now().UnixMilli(),
		MaxAttempts:    3,
		ConfigSnapshot: entity.JSON("{}"),
	}
	require.NoError(t, h.DB.Create(run).Error)

	return owned{
		account:  account,
		watcher:  watcher.ID,
		notifier: notifier,
		event:    watcher.Events[0].ID,
		match:    match.ID,
		run:      run.ID,
		session:  currentSessionID(t, h, user.Token),
	}
}

// currentSessionID reads the caller's own session, which is the id a stranger
// must not be able to revoke.
func currentSessionID(t *testing.T, h *support.Harness, token string) string {
	t.Helper()

	response := h.Get(t, "/api/users/_sessions", token)
	require.Equal(t, http.StatusOK, response.Status, response.Error(t))

	var sessions []struct {
		ID string `json:"id"`
	}
	response.Decode(t, &sessions)
	require.NotEmpty(t, sessions, "a logged-in user should have at least one session")

	return sessions[0].ID
}

func ptrTo[T any](value T) *T { return &value }

func TestOwnershipIsEnforcedOnEveryScopedRoute(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	user := h.Register(t, "isolation-owner@example.com", "secret123")
	stranger := h.Register(t, "isolation-stranger@example.com", "secret123")

	own := buildOwned(t, h, user)

	// A body is needed on the routes that validate one, or the 400 from the
	// validator would arrive before the ownership check and the test would
	// pass for the wrong reason.
	filters := map[string]any{"filters": []map[string]any{
		{"field": "subject", "operator": "contains", "value": "x"},
	}}
	event := map[string]any{"type": "notify", "run_mode": "immediate",
		"config": map[string]any{"message": "x"}}

	// name is the route template rather than the built path: a subtest named
	// after a generated uuid cannot be re-run with -run, and the output is
	// unreadable when thirty-seven of them scroll past.
	routes := []struct {
		method string
		name   string
		path   string
		body   any
	}{
		// ---- mail accounts
		{http.MethodGet, "/api/mail-accounts/:id", "/api/mail-accounts/" + own.account, nil},
		{http.MethodPatch, "/api/mail-accounts/:id", "/api/mail-accounts/" + own.account, map[string]any{"display_name": "stolen"}},
		{http.MethodDelete, "/api/mail-accounts/:id", "/api/mail-accounts/" + own.account, nil},
		{http.MethodGet, "/api/mail-accounts/:id/folders", "/api/mail-accounts/" + own.account + "/folders", nil},
		{http.MethodGet, "/api/mail-accounts/:id/sync-runs", "/api/mail-accounts/" + own.account + "/sync-runs", nil},
		{http.MethodPost, "/api/mail-accounts/:id/_verify", "/api/mail-accounts/" + own.account + "/_verify", nil},
		{http.MethodPost, "/api/mail-accounts/:id/_sync", "/api/mail-accounts/" + own.account + "/_sync", nil},
		{http.MethodPost, "/api/mail-accounts/:id/_reauthorize", "/api/mail-accounts/" + own.account + "/_reauthorize", nil},

		// ---- notifiers, which hold telegram tokens and webhook urls
		{http.MethodGet, "/api/notifiers/:id", "/api/notifiers/" + own.notifier, nil},
		{http.MethodPatch, "/api/notifiers/:id", "/api/notifiers/" + own.notifier, map[string]any{"name": "stolen"}},
		{http.MethodDelete, "/api/notifiers/:id", "/api/notifiers/" + own.notifier, nil},
		{http.MethodPost, "/api/notifiers/:id/_verify", "/api/notifiers/" + own.notifier + "/_verify", nil},
		{http.MethodPost, "/api/notifiers/:id/_test", "/api/notifiers/" + own.notifier + "/_test", nil},

		// ---- watchers
		{http.MethodGet, "/api/watchers/:id", "/api/watchers/" + own.watcher, nil},
		{http.MethodPatch, "/api/watchers/:id", "/api/watchers/" + own.watcher, map[string]any{"name": "stolen"}},
		{http.MethodDelete, "/api/watchers/:id", "/api/watchers/" + own.watcher, nil},
		{http.MethodPost, "/api/watchers/:id/_archive", "/api/watchers/" + own.watcher + "/_archive", nil},
		{http.MethodPost, "/api/watchers/:id/_restore", "/api/watchers/" + own.watcher + "/_restore", nil},
		{http.MethodPost, "/api/watchers/:id/_pause", "/api/watchers/" + own.watcher + "/_pause", nil},
		{http.MethodPost, "/api/watchers/:id/_resume", "/api/watchers/" + own.watcher + "/_resume", nil},
		{http.MethodPost, "/api/watchers/:id/_test", "/api/watchers/" + own.watcher + "/_test", nil},
		{http.MethodGet, "/api/watchers/:id/stats", "/api/watchers/" + own.watcher + "/stats", nil},

		// ---- filters
		{http.MethodGet, "/api/watchers/:id/filters", "/api/watchers/" + own.watcher + "/filters", nil},
		{http.MethodPut, "/api/watchers/:id/filters", "/api/watchers/" + own.watcher + "/filters", filters},

		// ---- events
		{http.MethodGet, "/api/watchers/:id/events", "/api/watchers/" + own.watcher + "/events", nil},
		{http.MethodPost, "/api/watchers/:id/events", "/api/watchers/" + own.watcher + "/events", event},
		{http.MethodPost, "/api/watchers/:id/events/_reorder", "/api/watchers/" + own.watcher + "/events/_reorder",
			map[string]any{"event_ids": []string{own.event}}},
		{http.MethodGet, "/api/watchers/:id/events/:eventId", "/api/watchers/" + own.watcher + "/events/" + own.event, nil},
		{http.MethodPatch, "/api/watchers/:id/events/:eventId", "/api/watchers/" + own.watcher + "/events/" + own.event,
			map[string]any{"config": map[string]any{"message": "stolen"}}},
		{http.MethodDelete, "/api/watchers/:id/events/:eventId", "/api/watchers/" + own.watcher + "/events/" + own.event, nil},
		{http.MethodPost, "/api/watchers/:id/events/:eventId/_test", "/api/watchers/" + own.watcher + "/events/" + own.event + "/_test", nil},

		// ---- activity. GET /matches/:matchId returns the mail itself, which
		// makes it the highest-consequence route in this table
		{http.MethodGet, "/api/matches/:id", "/api/matches/" + own.match, nil},
		{http.MethodGet, "/api/event-runs/:id", "/api/event-runs/" + own.run, nil},
		{http.MethodPost, "/api/event-runs/:id/_retry", "/api/event-runs/" + own.run + "/_retry", nil},
		{http.MethodPost, "/api/event-runs/:id/_cancel", "/api/event-runs/" + own.run + "/_cancel", nil},
		{http.MethodPost, "/api/event-runs/:id/_ack", "/api/event-runs/" + own.run + "/_ack", nil},

		// ---- sessions: a stranger must not be able to log someone out
		{http.MethodDelete, "/api/users/_sessions/:id", "/api/users/_sessions/" + own.session, nil},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.name, func(t *testing.T) {
			response := h.Do(t, route.method, route.path, stranger.Token, route.body)

			assert.Equal(t, http.StatusNotFound, response.Status,
				"a stranger reached %s %s and got %d; anything but 404 confirms the id is real",
				route.method, route.path, response.Status)
		})
	}

	// The owner is checked last, so a table that passes by refusing everybody
	// — a broken route, a dropped fixture — cannot be mistaken for isolation.
	t.Run("the owner still reaches everything", func(t *testing.T) {
		for _, path := range []string{
			"/api/mail-accounts/" + own.account,
			"/api/notifiers/" + own.notifier,
			"/api/watchers/" + own.watcher,
			"/api/watchers/" + own.watcher + "/filters",
			"/api/watchers/" + own.watcher + "/events/" + own.event,
			"/api/matches/" + own.match,
			"/api/event-runs/" + own.run,
		} {
			assert.Equal(t, http.StatusOK, h.Get(t, path, user.Token).Status, path)
		}
	})
}

// A stranger's id and an id that never existed must be indistinguishable, or
// the 404 above is only hiding the row rather than hiding its existence.
func TestUnknownIdsAnswerTheSameAsForeignOnes(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	user := h.Register(t, "unknown-ids@example.com", "secret123")
	missing := uuid.NewString()

	for _, path := range []string{
		"/api/mail-accounts/" + missing,
		"/api/notifiers/" + missing,
		"/api/watchers/" + missing,
		"/api/matches/" + missing,
		"/api/event-runs/" + missing,
	} {
		assert.Equal(t, http.StatusNotFound, h.Get(t, path, user.Token).Status, path)
	}
}

// Every admin route, including the ones that change or take over an account.
// The read-only routes were already covered; the mutating ones are where a
// missing role check does real damage, and _impersonate is the worst of them.
func TestAdminMutationsRequireSuperadmin(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	plain := h.Register(t, "not-an-admin@example.com", "secret123")
	victim := h.Register(t, "admin-target@example.com", "secret123")

	routes := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/admin/users/" + victim.ID, nil},
		{http.MethodPatch, "/api/admin/users/" + victim.ID, map[string]any{"name": "renamed"}},
		{http.MethodPost, "/api/admin/users/" + victim.ID + "/_suspend", nil},
		{http.MethodPost, "/api/admin/users/" + victim.ID + "/_restore", nil},
		{http.MethodPost, "/api/admin/users/" + victim.ID + "/_impersonate", nil},
	}

	t.Run("a plain user is refused", func(t *testing.T) {
		for _, route := range routes {
			response := h.Do(t, route.method, route.path, plain.Token, route.body)

			assert.Equal(t, http.StatusForbidden, response.Status,
				"%s %s let a non-admin through", route.method, route.path)
		}
	})

	t.Run("and nothing happened to the target", func(t *testing.T) {
		user := new(entity.User)
		require.NoError(t, h.DB.Where("id = ?", victim.ID).Take(user).Error)

		assert.Equal(t, entity.UserStatusActive, user.Status,
			"the refused _suspend changed the user anyway")
		assert.NotEqual(t, "renamed", user.Name,
			"the refused patch changed the user anyway")
	})

	t.Run("a superadmin is allowed", func(t *testing.T) {
		admin := h.Register(t, "real-admin@example.com", "secret123")
		h.MakeSuperadmin(t, admin.ID)

		// the role rides along in the cached auth, so a fresh token is needed
		token := h.Login(t, "real-admin@example.com", "secret123")

		response := h.Get(t, "/api/admin/users/"+victim.ID, token)
		assert.Equal(t, http.StatusOK, response.Status, response.Error(t))
	})
}
