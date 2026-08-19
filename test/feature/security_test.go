//go:build feature

// Session revocation, impersonation attribution and the credential-endpoint
// rate limits. These are the three things that were known-broken rather than
// merely missing, so each test is written to fail against the old behaviour.
package feature

import (
	"fmt"
	"net/http"
	"testing"

	"mailpulse/test/support"

	"github.com/stretchr/testify/require"
)

// warm makes an authenticated request so the token is definitely in the redis
// cache. Without this the revocation tests below pass for the wrong reason:
// there would be no cache entry to fail to evict.
func warm(t *testing.T, h *support.Harness, token string) {
	t.Helper()

	response := h.Get(t, "/api/users/_current", token)
	require.Equal(t, http.StatusOK, response.Status, "token should work before it is revoked")
}

func TestRevokedSessionStopsWorkingImmediately(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	account := h.Register(t, "revoker@example.com", "secret123")

	// a second session, as if the user had signed in on another device
	other := h.Login(t, "revoker@example.com", "secret123")

	// both have to be cached before the revoke, and account.Token especially:
	// it is the one being revoked, and an uncached token falls through to the
	// database and gets rejected whether or not eviction works
	warm(t, h, account.Token)
	warm(t, h, other)

	var sessions []struct {
		ID      string `json:"id"`
		Current bool   `json:"current"`
	}
	h.Get(t, "/api/users/_sessions", other).Decode(t, &sessions)
	require.Len(t, sessions, 2)

	// revoke the *other* device from this one, which is the case that could not
	// evict: only the token hash is recoverable, never the raw token
	var target string
	for _, session := range sessions {
		if !session.Current {
			target = session.ID
		}
	}
	require.NotEmpty(t, target, "expected one session that is not the caller's own")

	revoked := h.Delete(t, "/api/users/_sessions/"+target, other)
	require.Equal(t, http.StatusOK, revoked.Status)

	// the revoked device is account.Token, and it must be dead now rather than
	// at the end of REDIS_TTL_AUTH
	after := h.Get(t, "/api/users/_current", account.Token)
	require.Equal(t, http.StatusUnauthorized, after.Status,
		"a revoked session kept working, so the cache was not evicted")

	// the caller's own session is untouched
	still := h.Get(t, "/api/users/_current", other)
	require.Equal(t, http.StatusOK, still.Status)
}

func TestPasswordChangeEvictsEverySession(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	account := h.Register(t, "rotator@example.com", "secret123")
	other := h.Login(t, "rotator@example.com", "secret123")

	warm(t, h, account.Token)
	warm(t, h, other)

	changed := h.Patch(t, "/api/users/_current", account.Token, map[string]string{
		"password": "brandnew123",
	})
	require.Equal(t, http.StatusOK, changed.Status)

	// changing a credential revokes every session; both were cached, so both
	// have to be evicted rather than left to expire
	for name, token := range map[string]string{"acting": account.Token, "other": other} {
		response := h.Get(t, "/api/users/_current", token)
		require.Equal(t, http.StatusUnauthorized, response.Status,
			"%s session survived a password change", name)
	}

	require.NotEmpty(t, h.Login(t, "rotator@example.com", "brandnew123"))
}

func TestImpersonatedActionsAreAttributedToTheAdmin(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	admin := h.Register(t, "admin@example.com", "secret123")
	h.MakeSuperadmin(t, admin.ID)
	adminToken := h.Login(t, "admin@example.com", "secret123")

	target := h.Register(t, "target@example.com", "secret123")

	var impersonation struct {
		Token string `json:"token"`
	}
	started := h.Post(t, "/api/admin/users/"+target.ID+"/_impersonate", adminToken, nil)
	require.Equal(t, http.StatusOK, started.Status)
	started.Decode(t, &impersonation)

	// act as the target user through the impersonated session
	created := h.Post(t, "/api/notifiers", impersonation.Token, map[string]any{
		"name": "made while impersonating", "type": "webhook",
		"config": map[string]any{"url": "https://example.com/hook"},
	})
	require.Equal(t, http.StatusCreated, created.Status)

	var logs []struct {
		ActorUserID        *string `json:"actor_user_id"`
		ImpersonatedUserID *string `json:"impersonated_user_id"`
		Action             string  `json:"action"`
	}
	h.Get(t, "/api/admin/audit-logs?size=50", adminToken).Decode(t, &logs)

	var found bool
	for _, entry := range logs {
		if entry.Action != "notifier.created" {
			continue
		}
		found = true

		// the whole point: the row names the admin who is really acting, with
		// the impersonated user recorded alongside. Before this it named the
		// target and was indistinguishable from their own activity.
		require.NotNil(t, entry.ActorUserID)
		require.Equal(t, admin.ID, *entry.ActorUserID,
			"the acting admin should own the audit row")
		require.NotNil(t, entry.ImpersonatedUserID,
			"an action taken while impersonating must be marked as such")
		require.Equal(t, target.ID, *entry.ImpersonatedUserID)
	}
	require.True(t, found, "no audit row was written for the impersonated action")
}

func TestOrdinaryActionsAreNotMarkedImpersonated(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	admin := h.Register(t, "plain-admin@example.com", "secret123")
	h.MakeSuperadmin(t, admin.ID)
	adminToken := h.Login(t, "plain-admin@example.com", "secret123")

	created := h.Post(t, "/api/notifiers", adminToken, map[string]any{
		"name": "made normally", "type": "webhook",
		"config": map[string]any{"url": "https://example.com/hook"},
	})
	require.Equal(t, http.StatusCreated, created.Status)

	var logs []struct {
		ActorUserID        *string `json:"actor_user_id"`
		ImpersonatedUserID *string `json:"impersonated_user_id"`
		Action             string  `json:"action"`
	}
	h.Get(t, "/api/admin/audit-logs?size=50", adminToken).Decode(t, &logs)

	for _, entry := range logs {
		if entry.Action == "notifier.created" {
			require.Nil(t, entry.ImpersonatedUserID,
				"a normal action must not be tagged as impersonated")
			require.Equal(t, admin.ID, *entry.ActorUserID)
		}
	}
}

func TestImpersonationCannotBeChained(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	admin := h.Register(t, "chain-admin@example.com", "secret123")
	h.MakeSuperadmin(t, admin.ID)
	adminToken := h.Login(t, "chain-admin@example.com", "secret123")

	// the target is also a superadmin, so the impersonated session carries the
	// role that would otherwise let it impersonate onwards
	target := h.Register(t, "chain-target@example.com", "secret123")
	h.MakeSuperadmin(t, target.ID)

	var impersonation struct {
		Token string `json:"token"`
	}
	started := h.Post(t, "/api/admin/users/"+target.ID+"/_impersonate", adminToken, nil)
	require.Equal(t, http.StatusOK, started.Status)
	started.Decode(t, &impersonation)

	third := h.Register(t, "chain-third@example.com", "secret123")

	// a session records one impersonator, so a chain would collapse and the
	// audit trail would name the wrong admin
	chained := h.Post(t, "/api/admin/users/"+third.ID+"/_impersonate", impersonation.Token, nil)
	require.Equal(t, http.StatusForbidden, chained.Status)
}

func TestLoginIsRateLimited(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	h.Register(t, "throttled@example.com", "secret123")

	limit := h.Config.GetInt("security.ratelimit.login.attempts")
	require.Greater(t, limit, 0, "the limit must be configured for this test to mean anything")

	// wrong password throughout: the limiter runs before the handler, so what
	// is being counted is attempts, not failures
	var lastStatus int
	for attempt := 0; attempt < limit+2; attempt++ {
		response := h.Post(t, "/api/users/_login", "", map[string]string{
			"email": "throttled@example.com", "password": "wrong-password",
		})
		lastStatus = response.Status

		if attempt < limit-1 {
			require.Equal(t, http.StatusUnauthorized, lastStatus,
				"attempt %d was throttled before the limit", attempt+1)
		}
	}

	require.Equal(t, http.StatusTooManyRequests, lastStatus,
		fmt.Sprintf("login was still accepted after %d attempts", limit+2))

	// and the correct password does not buy a way past the limiter
	blocked := h.Post(t, "/api/users/_login", "", map[string]string{
		"email": "throttled@example.com", "password": "secret123",
	})
	require.Equal(t, http.StatusTooManyRequests, blocked.Status)
}

// The limiter is only worth having if the identity it counts against cannot be
// chosen by the caller. Nothing is listed in WEB_TRUSTED_PROXIES here, so a
// forged X-Forwarded-For must be ignored and every attempt must land in the
// same bucket.
func TestRateLimitCannotBeEvadedByForgingForwardedFor(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	h.Register(t, "spoofer@example.com", "secret123")

	limit := h.Config.GetInt("security.ratelimit.login.attempts")

	var lastStatus int
	for attempt := 0; attempt < limit+2; attempt++ {
		// a different claimed source address every single time
		response := h.DoWithHeaders(t, http.MethodPost, "/api/users/_login", "",
			map[string]string{"email": "spoofer@example.com", "password": "wrong-password"},
			map[string]string{"X-Forwarded-For": fmt.Sprintf("203.0.113.%d", attempt+1)})
		lastStatus = response.Status
	}

	require.Equal(t, http.StatusTooManyRequests, lastStatus,
		"changing X-Forwarded-For bought more attempts, so the limiter counts a header the caller controls")
}

func TestForgotPasswordIsRateLimited(t *testing.T) {
	h := support.New(t)
	h.Reset(t)

	h.Register(t, "forgetful@example.com", "secret123")

	limit := h.Config.GetInt("security.ratelimit.forgot_password.attempts")
	require.Greater(t, limit, 0)

	var lastStatus int
	for attempt := 0; attempt < limit+2; attempt++ {
		response := h.Post(t, "/api/users/_forgot-password", "", map[string]string{
			"email": "forgetful@example.com",
		})
		lastStatus = response.Status
	}

	require.Equal(t, http.StatusTooManyRequests, lastStatus,
		"forgot-password kept sending mail past its limit")
}
