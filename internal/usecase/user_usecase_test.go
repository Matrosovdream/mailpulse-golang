package usecase

import (
	"testing"
	"time"

	"mailpulse/internal/entity"
)

// shouldTouch decides whether Verify writes last_used_at on a cache miss. The
// interval it compares against is in seconds-as-a-Duration while the column is
// unix milliseconds, so the unit conversion is the whole risk here: getting it
// wrong by a factor of a thousand either writes every time or never again.
func TestShouldTouch(t *testing.T) {
	now := time.Now().UnixMilli()

	cases := map[string]struct {
		interval time.Duration
		lastUsed int64
		want     bool
	}{
		// zero keeps the original behaviour: a write on every miss
		"disabled, just written": {0, now, true},
		"disabled, long ago":     {0, now - 86_400_000, true},

		"fresh, well inside the interval": {5 * time.Minute, now - 30_000, false},
		"stale, well past the interval":   {5 * time.Minute, now - 600_000, true},

		// the boundary itself counts as due, so an interval of n means the
		// column is never more than n stale rather than n plus a tick
		"exactly at the interval": {5 * time.Minute, now - 300_000, true},
		"a second short of it":    {5 * time.Minute, now - 299_000, false},

		// a session created and immediately used has last_used_at at or near
		// now; it must not be written again straight away
		"brand new session": {5 * time.Minute, now, false},

		// clock skew between processes can put the stored value in the future.
		// The comparison must not then decide the row is overdue.
		"timestamp in the future": {5 * time.Minute, now + 60_000, false},
	}

	for name, testCase := range cases {
		useCase := &UserUseCase{TouchInterval: testCase.interval}
		session := &entity.UserSession{LastUsedAt: testCase.lastUsed}

		if got := useCase.shouldTouch(session, now); got != testCase.want {
			t.Errorf("%s: shouldTouch() = %v, want %v", name, got, testCase.want)
		}
	}
}
