package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"mailpulse/internal/model"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// OAuthStateKeyPrefix namespaces the pending consent handshakes.
const OAuthStateKeyPrefix = "oauth:state"

// OAuthStateCache holds the state parameter issued at _authorize until the
// provider redirects back.
//
// The state is the only thing tying a callback to a user: that route has no
// session, because the request arrives from the provider's redirect. So the
// value stored here is what decides whose mailbox gets connected, which is why
// it is single use and short lived.
type OAuthStateCache struct {
	Cache[model.OAuthState]
}

func NewOAuthStateCache(client *redis.Client, log *logrus.Logger, ttl time.Duration) *OAuthStateCache {
	return &OAuthStateCache{
		Cache: Cache[model.OAuthState]{
			Client: client,
			Prefix: OAuthStateKeyPrefix,
			TTL:    ttl,
			Log:    log,
		},
	}
}

// Consume reads a state and deletes it in the same operation, returning nil
// when it was not there.
//
// This is GETDEL rather than the inherited Get followed by Delete on purpose.
// A callback URL sits in browser history and server logs, so it gets replayed;
// with a read and a separate delete, two replays that arrive together both see
// the state and both go on to spend the authorization code. One atomic command
// means exactly one of them can win, and the loser is told the link expired.
func (c *OAuthStateCache) Consume(ctx context.Context, state string) (*model.OAuthState, error) {
	value, err := c.Client.GetDel(ctx, c.Key(state)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		c.Log.WithError(err).Warn("failed to consume an oauth state")
		return nil, err
	}

	entry := new(model.OAuthState)
	if err := json.Unmarshal(value, entry); err != nil {
		c.Log.WithError(err).Warn("failed to unmarshal an oauth state")
		return nil, err
	}

	return entry, nil
}
