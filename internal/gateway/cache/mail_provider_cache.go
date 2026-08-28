package cache

import (
	"context"
	"time"

	"mailpulse/internal/entity"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// MailProviderKeyPrefix namespaces the slug -> mail_providers row entries.
const MailProviderKeyPrefix = "mail:provider"

// MailProviderCache holds the mail_providers row behind a slug.
//
// Every mailbox resolve reads this row — the poller resolves each account on
// every cycle, so at ten thousand accounts on a two minute interval it is
// roughly eighty identical single-row lookups a second, forever. The table has
// seven rows and changes when someone runs a seeder.
//
// It caches misses as well as hits. An unknown slug is a 400, and without a
// negative entry a client looping on a bad slug would reach the database on
// every attempt — the one case where a cache that only stores successes makes
// the load it was added to remove.
type MailProviderCache struct {
	Cache[entry]
}

// entry wraps the row so a cached "this slug does not exist" is representable.
// A nil Provider with Found false is the negative case.
type entry struct {
	Provider *entity.MailProvider `json:"provider,omitempty"`
	Found    bool                 `json:"found"`
}

func NewMailProviderCache(client *redis.Client, log *logrus.Logger, ttl time.Duration) *MailProviderCache {
	return &MailProviderCache{
		Cache: Cache[entry]{
			Client: client,
			Prefix: MailProviderKeyPrefix,
			TTL:    ttl,
			Log:    log,
		},
	}
}

// Lookup returns the row, whether the slug is known, and whether the answer
// came from the cache at all.
//
// The third value matters: a redis failure has to be distinguishable from a
// cached miss, or an unreachable cache would start reporting every provider as
// unknown and park every mailbox in the fleet.
func (c *MailProviderCache) Lookup(ctx context.Context, slug string) (*entity.MailProvider, bool, bool) {
	cached, err := c.Get(ctx, slug)
	if err != nil || cached == nil {
		return nil, false, false
	}
	return cached.Provider, cached.Found, true
}

// Remember stores a row. A nil provider records the negative answer.
func (c *MailProviderCache) Remember(ctx context.Context, slug string, provider *entity.MailProvider) {
	_ = c.Set(ctx, slug, &entry{Provider: provider, Found: provider != nil})
}

// Forget drops one slug, for a seeder or an admin edit that changes a row.
func (c *MailProviderCache) Forget(ctx context.Context, slug string) {
	_ = c.Delete(ctx, slug)
}
