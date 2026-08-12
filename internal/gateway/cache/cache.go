package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// Cache is a generic JSON backed redis cache for a single kind of value.
// Keys are namespaced with Prefix so every cache owns its own key space.
type Cache[T any] struct {
	Client *redis.Client
	Prefix string
	TTL    time.Duration
	Log    *logrus.Logger
}

func (c *Cache[T]) Key(id string) string {
	return c.Prefix + ":" + id
}

// Get returns a nil value without an error when the key is not cached.
func (c *Cache[T]) Get(ctx context.Context, id string) (*T, error) {
	value, err := c.Client.Get(ctx, c.Key(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		c.Log.WithError(err).Warnf("failed to get cache %s", c.Key(id))
		return nil, err
	}

	entry := new(T)
	if err := json.Unmarshal(value, entry); err != nil {
		c.Log.WithError(err).Warnf("failed to unmarshal cache %s", c.Key(id))
		return nil, err
	}

	return entry, nil
}

func (c *Cache[T]) Set(ctx context.Context, id string, entry *T) error {
	value, err := json.Marshal(entry)
	if err != nil {
		c.Log.WithError(err).Warnf("failed to marshal cache %s", c.Key(id))
		return err
	}

	if err := c.Client.Set(ctx, c.Key(id), value, c.TTL).Err(); err != nil {
		c.Log.WithError(err).Warnf("failed to set cache %s", c.Key(id))
		return err
	}

	return nil
}

func (c *Cache[T]) Delete(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}

	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = c.Key(id)
	}

	if err := c.Client.Del(ctx, keys...).Err(); err != nil {
		c.Log.WithError(err).Warnf("failed to delete cache %v", keys)
		return err
	}

	return nil
}
