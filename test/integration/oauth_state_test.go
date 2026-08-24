//go:build integration

package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"mailpulse/internal/gateway/cache"
	"mailpulse/internal/model"
	"mailpulse/test/support"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stateCache(t *testing.T, ttl time.Duration) *cache.OAuthStateCache {
	t.Helper()
	h := support.New(t)
	return cache.NewOAuthStateCache(h.Redis, h.Log, ttl)
}

func TestOAuthStateCache_RoundTrip(t *testing.T) {
	states := stateCache(t, time.Minute)
	ctx := context.Background()

	stored := &model.OAuthState{UserID: "user-1", Provider: "yandex", CreatedAt: time.Now().UnixMilli()}
	require.NoError(t, states.Set(ctx, "state-round-trip", stored))

	got, err := states.Consume(ctx, "state-round-trip")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, "user-1", got.UserID)
	assert.Equal(t, "yandex", got.Provider)
}

// A state that was never issued has to be indistinguishable from one already
// spent: both come back nil, and the callback answers the same way to each.
func TestOAuthStateCache_MissingStateIsNotAnError(t *testing.T) {
	states := stateCache(t, time.Minute)

	got, err := states.Consume(context.Background(), "never-issued")

	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestOAuthStateCache_IsSingleUse(t *testing.T) {
	states := stateCache(t, time.Minute)
	ctx := context.Background()

	require.NoError(t, states.Set(ctx, "state-once", &model.OAuthState{UserID: "user-1", Provider: "yandex"}))

	first, err := states.Consume(ctx, "state-once")
	require.NoError(t, err)
	require.NotNil(t, first)

	second, err := states.Consume(ctx, "state-once")
	require.NoError(t, err)
	assert.Nil(t, second, "a callback URL sits in browser history and gets replayed")
}

// This is what GETDEL buys over a read followed by a delete.
//
// A callback URL carries a working authorization code and lands in history,
// logs and referrers, so it gets replayed — sometimes simultaneously. With two
// separate commands both replays see the state, both go on to spend the code,
// and the second one either creates a duplicate mailbox or errors in front of
// the user. One atomic command means exactly one winner.
func TestOAuthStateCache_OnlyOneConcurrentConsumerWins(t *testing.T) {
	states := stateCache(t, time.Minute)
	ctx := context.Background()

	const racers = 25

	require.NoError(t, states.Set(ctx, "state-contended",
		&model.OAuthState{UserID: "user-1", Provider: "yandex"}))

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		winners int
	)

	start := make(chan struct{})

	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			got, err := states.Consume(ctx, "state-contended")
			if err == nil && got != nil {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}()
	}

	close(start)
	wg.Wait()

	assert.Equal(t, 1, winners, "%d racers consumed the same state", racers)
}

func TestOAuthStateCache_Expires(t *testing.T) {
	states := stateCache(t, 300*time.Millisecond)
	ctx := context.Background()

	require.NoError(t, states.Set(ctx, "state-expiring", &model.OAuthState{UserID: "user-1", Provider: "yandex"}))

	time.Sleep(500 * time.Millisecond)

	got, err := states.Consume(ctx, "state-expiring")
	require.NoError(t, err)
	assert.Nil(t, got, "a consent screen left open all day must not still be redeemable")
}

// The lock is what stops N workers refreshing one account's token N times.
func TestLock_OnlyOneHolder(t *testing.T) {
	h := support.New(t)
	locks := cache.NewLock(h.Redis, h.Log)
	ctx := context.Background()

	h.Redis.Del(ctx, "lock:oauth:refresh:account-1")

	assert.True(t, locks.Acquire(ctx, "oauth:refresh", "account-1", time.Minute))
	assert.False(t, locks.Acquire(ctx, "oauth:refresh", "account-1", time.Minute),
		"the second worker has to wait for the first one's write instead of refreshing too")

	locks.Release(ctx, "oauth:refresh", "account-1")

	assert.True(t, locks.Acquire(ctx, "oauth:refresh", "account-1", time.Minute),
		"releasing has to actually free it, or one crashed refresh stalls the mailbox for the whole TTL")

	locks.Release(ctx, "oauth:refresh", "account-1")
}
