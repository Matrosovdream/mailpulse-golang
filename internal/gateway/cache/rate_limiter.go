package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// RateLimitKeyPrefix namespaces the counters in redis.
const RateLimitKeyPrefix = "ratelimit"

// RateLimiter is a fixed-window counter: the first request in a window sets the
// expiry, and the window only moves once it has fully lapsed.
//
// A sliding window would be fairer at the boundary — a caller can spend a full
// allowance at the end of one window and again at the start of the next — but
// it costs a sorted set per caller and a cleanup pass. For slowing down
// credential guessing, a factor of two at the seam does not matter.
type RateLimiter struct {
	Client *redis.Client
	Log    *logrus.Logger
}

func NewRateLimiter(client *redis.Client, log *logrus.Logger) *RateLimiter {
	return &RateLimiter{Client: client, Log: log}
}

func (r *RateLimiter) key(scope, id string) string {
	return RateLimitKeyPrefix + ":" + scope + ":" + id
}

// Allow counts one hit against scope/id and reports whether it is still within
// limit, along with how long until the window resets.
//
// It fails open. Redis being unreachable should not take down login for
// everyone: the counter is a brake on credential guessing, not an
// authorisation decision, and refusing every request would convert a cache
// outage into a full outage.
func (r *RateLimiter) Allow(ctx context.Context, scope, id string, limit int, window time.Duration) (bool, time.Duration) {
	key := r.key(scope, id)

	count, err := r.Client.Incr(ctx, key).Result()
	if err != nil {
		r.Log.WithError(err).Warnf("rate limiter unavailable, allowing %s", key)
		return true, 0
	}

	// only the request that created the counter sets the expiry, otherwise a
	// steady stream of requests would push the window out indefinitely and the
	// caller would never be let back in
	if count == 1 {
		if err := r.Client.Expire(ctx, key, window).Err(); err != nil {
			r.Log.WithError(err).Warnf("failed to set rate limit expiry on %s", key)
		}
		return true, window
	}

	if count > int64(limit) {
		retryAfter, err := r.Client.TTL(ctx, key).Result()
		if err != nil || retryAfter < 0 {
			retryAfter = window
		}
		return false, retryAfter
	}

	return true, window
}
