package cache

import (
	"time"

	"mailpulse/internal/model"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// DashboardKeyPrefix namespaces the per-user dashboard rollups.
const DashboardKeyPrefix = "dashboard:summary"

// DashboardCache holds one user's dashboard rollup.
//
// The rollup is six queries — four status counts and two aggregates over a
// 24 hour window — and it backs the SPA's landing view, which makes it the
// most requested expensive endpoint in the API. Nothing on that page needs to
// be current to the second: the counts move when a watcher fires, and no
// reader can tell a thirty second old count from a live one.
//
// There is deliberately no invalidation. A short TTL is the entire mechanism,
// because the writers that would have to evict — the poller recording a match,
// the dispatcher settling a run — run in a different process from the reader,
// and wiring eviction across that boundary buys freshness nobody asked for. A
// briefly stale count is not a correctness problem the way a stale
// authorisation would be, which is why the same reasoning does not apply to
// [UserCache].
//
// Keyed by user id, so the entry is scoped to exactly the tenant whose data it
// holds and there is no key a second user could reach.
type DashboardCache struct {
	Cache[model.DashboardSummaryResponse]
}

func NewDashboardCache(client *redis.Client, log *logrus.Logger, ttl time.Duration) *DashboardCache {
	return &DashboardCache{
		Cache: Cache[model.DashboardSummaryResponse]{
			Client: client,
			Prefix: DashboardKeyPrefix,
			TTL:    ttl,
			Log:    log,
		},
	}
}
