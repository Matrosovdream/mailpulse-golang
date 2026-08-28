//go:build feature

package feature

import (
	"net/http"
	"testing"

	"mailpulse/test/support"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type adminUser struct {
	ID     string `json:"id"`
	Email  string `json:"email"`
	Counts struct {
		Watchers     int64 `json:"watchers"`
		MailAccounts int64 `json:"mail_accounts"`
		Notifiers    int64 `json:"notifiers"`
		Matches      int64 `json:"matches"`
	} `json:"counts"`
}

// The per-user counts on the admin list are gathered for the whole page in
// four grouped queries rather than four per row. That is a rewrite of how the
// numbers are produced, so what needs pinning is that they are still the right
// numbers — including the zero for a user who owns nothing, which is now an
// absent map key rather than a query returning 0.
func TestAdminUserListCounts(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	admin := h.Register(t, "counts-admin@example.com", "secret123")
	h.MakeSuperadmin(t, admin.ID)
	token := h.Login(t, "counts-admin@example.com", "secret123")

	// one user with things, one with nothing
	busy := h.Register(t, "counts-busy@example.com", "secret123")
	h.Register(t, "counts-empty@example.com", "secret123")

	account := connectedAccount(t, h, busy.Token, "counts-mailbox@example.com")
	notifier := verifiedNotifier(t, h, busy.Token, "counts notifier")

	for _, name := range []string{"first", "second", "third"} {
		created := h.Post(t, "/api/watchers", busy.Token, map[string]any{
			"mail_account_id": account, "name": name,
			"events": []map[string]any{
				{"type": "notify", "run_mode": "immediate",
					"config":       map[string]any{"message": "hi"},
					"notifier_ids": []string{notifier}},
			},
		})
		require.Equal(t, http.StatusCreated, created.Status, created.Error(t))
	}

	response := h.Get(t, "/api/admin/users?page=1&size=100", token)
	require.Equal(t, http.StatusOK, response.Status, response.Error(t))

	var users []adminUser
	response.Decode(t, &users)

	byEmail := map[string]adminUser{}
	for _, user := range users {
		byEmail[user.Email] = user
	}

	t.Run("a user's own rows are counted", func(t *testing.T) {
		got, ok := byEmail["counts-busy@example.com"]
		require.True(t, ok, "the busy user is missing from the page")

		assert.Equal(t, int64(3), got.Counts.Watchers)
		assert.Equal(t, int64(1), got.Counts.MailAccounts)
		assert.Equal(t, int64(1), got.Counts.Notifiers)
		assert.Equal(t, int64(0), got.Counts.Matches, "nothing has been synced yet")
	})

	// The grouped query returns no row at all for a user who owns nothing, so
	// the zero comes from a missing map key. That is exactly the case the
	// rewrite could have turned into a wrong number or a panic.
	t.Run("a user who owns nothing counts zero", func(t *testing.T) {
		got, ok := byEmail["counts-empty@example.com"]
		require.True(t, ok, "the empty user is missing from the page")

		assert.Equal(t, int64(0), got.Counts.Watchers)
		assert.Equal(t, int64(0), got.Counts.MailAccounts)
		assert.Equal(t, int64(0), got.Counts.Notifiers)
		assert.Equal(t, int64(0), got.Counts.Matches)
	})

	// Counts must not leak across users: the grouped query fetches every
	// listed user's rows in one pass, so a mistake in the grouping would show
	// up as one user carrying another's totals.
	t.Run("counts are not shared between users", func(t *testing.T) {
		got, ok := byEmail["counts-admin@example.com"]
		require.True(t, ok)

		assert.Equal(t, int64(0), got.Counts.Watchers,
			"the admin owns nothing, so a non-zero count means the grouping is wrong")
	})

	// GetUser still uses the single-user path, and the two must agree.
	t.Run("the detail endpoint agrees with the list", func(t *testing.T) {
		listed := byEmail["counts-busy@example.com"]

		detail := h.Get(t, "/api/admin/users/"+listed.ID, token)
		require.Equal(t, http.StatusOK, detail.Status, detail.Error(t))

		var one adminUser
		detail.Decode(t, &one)

		assert.Equal(t, listed.Counts, one.Counts,
			"the list and the detail endpoint disagree about the same user")
	})
}
