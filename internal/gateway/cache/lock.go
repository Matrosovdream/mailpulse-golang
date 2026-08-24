package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// LockKeyPrefix namespaces the short-lived mutexes.
const LockKeyPrefix = "lock"

// Lock is a best-effort mutex across processes, built on SETNX with an expiry.
//
// It is not a correctness primitive and is not trying to be one: there is no
// fencing token, and a holder that stalls past the TTL loses the lock without
// knowing. It exists to stop N workers doing the same idempotent piece of work
// at the same moment — refreshing one account's OAuth token — where doing it
// twice is wasteful rather than wrong.
type Lock struct {
	Client *redis.Client
	Log    *logrus.Logger
}

func NewLock(client *redis.Client, log *logrus.Logger) *Lock {
	return &Lock{Client: client, Log: log}
}

func (l *Lock) key(scope, id string) string {
	return LockKeyPrefix + ":" + scope + ":" + id
}

// Acquire reports whether the caller now holds the lock.
//
// It fails open, like the rate limiter and for the same reason: if redis is
// unreachable, refusing to refresh would stop every mailbox syncing, whereas
// two workers refreshing the same token independently costs one extra round
// trip to the provider and leaves valid tokens either way.
func (l *Lock) Acquire(ctx context.Context, scope, id string, ttl time.Duration) bool {
	acquired, err := l.Client.SetNX(ctx, l.key(scope, id), "1", ttl).Result()
	if err != nil {
		l.Log.WithError(err).Warnf("lock unavailable, proceeding without it: %s", l.key(scope, id))
		return true
	}
	return acquired
}

// Release drops the lock early. Missing it is survivable — the TTL is the real
// guarantee — so the error is logged rather than returned.
func (l *Lock) Release(ctx context.Context, scope, id string) {
	if err := l.Client.Del(ctx, l.key(scope, id)).Err(); err != nil {
		l.Log.WithError(err).Warnf("failed to release lock %s", l.key(scope, id))
	}
}
