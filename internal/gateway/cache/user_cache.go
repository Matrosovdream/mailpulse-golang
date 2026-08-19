package cache

import (
	"time"

	"mailpulse/internal/model"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// UserCacheKeyPrefix namespaces the token hash -> auth entries in redis.
const UserCacheKeyPrefix = "user:token"

// PasswordResetKeyPrefix namespaces one-shot password reset tokens.
const PasswordResetKeyPrefix = "password:reset"

// UserCache caches the session lookup done on every authenticated request, so
// the auth middleware does not hit the database each time.
//
// Keys are the SHA-256 hash of the bearer token, matching
// user_sessions.token_hash. That is deliberate: revocation only ever holds the
// hash, so keying by the raw token made it impossible to evict the entry for a
// session being revoked from another device.
//
// Because the cached Auth carries the user's roles, anything that changes a
// user's roles or revokes a session has to evict here too — otherwise the old
// answer keeps being served until the TTL lapses.
type UserCache struct {
	Cache[model.Auth]
}

func NewUserCache(client *redis.Client, log *logrus.Logger, ttl time.Duration) *UserCache {
	return &UserCache{
		Cache: Cache[model.Auth]{
			Client: client,
			Prefix: UserCacheKeyPrefix,
			TTL:    ttl,
			Log:    log,
		},
	}
}

// PasswordResetCache holds reset tokens with their own expiry. Redis is the
// right store: they are single use, short lived, and worthless after a restart.
type PasswordResetCache struct {
	Cache[model.PasswordReset]
}

func NewPasswordResetCache(client *redis.Client, log *logrus.Logger, ttl time.Duration) *PasswordResetCache {
	return &PasswordResetCache{
		Cache: Cache[model.PasswordReset]{
			Client: client,
			Prefix: PasswordResetKeyPrefix,
			TTL:    ttl,
			Log:    log,
		},
	}
}
