package cache

import (
	"mailpulse/internal/model"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// UserCacheKeyPrefix namespaces the token -> auth entries in redis.
const UserCacheKeyPrefix = "user:token"

// UserCache caches the token verification done on every authenticated request,
// so the auth middleware does not hit the database each time.
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
