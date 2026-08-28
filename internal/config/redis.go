package config

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// NewRedis builds the client from env alone, so the cache can live on this
// machine or on another one without a code change.
//
// The options beyond host and port exist for the second case. A Redis reached
// over a network you do not own needs TLS, usually an ACL username as well as
// a password, and timeouts chosen for that link rather than go-redis's
// LAN-shaped defaults.
func NewRedis(config *viper.Viper, log *logrus.Logger) *redis.Client {
	options := &redis.Options{
		Addr:     fmt.Sprintf("%s:%d", config.GetString("redis.host"), config.GetInt("redis.port")),
		Username: config.GetString("redis.username"),
		Password: config.GetString("redis.password"),
		DB:       config.GetInt("redis.db"),
		PoolSize: config.GetInt("redis.pool.size"),

		DialTimeout:  duration(config, "redis.timeout.dial"),
		ReadTimeout:  duration(config, "redis.timeout.read"),
		WriteTimeout: duration(config, "redis.timeout.write"),
		MaxRetries:   config.GetInt("redis.max_retries"),
	}

	if config.GetBool("redis.tls") {
		// Managed Redis — ElastiCache with encryption in transit, Upstash,
		// Redis Cloud, Azure Cache — refuses plaintext. ServerName has to be
		// the host we dialled or verification fails against the certificate.
		options.TLSConfig = &tls.Config{
			ServerName:         config.GetString("redis.host"),
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: config.GetBool("redis.tls_insecure"),
		}

		if options.TLSConfig.InsecureSkipVerify {
			log.Warn("REDIS_TLS_INSECURE is on: the Redis certificate is not being verified")
		}
	}

	client := redis.NewClient(options)

	if err := pingRedis(client, config, log); err != nil {
		log.Fatalf("failed to connect redis at %s: %v", options.Addr, err)
	}

	return client
}

// pingRedis retries before giving up.
//
// A single attempt is right for a cache in the same compose file, where
// depends_on has already waited for it. It is wrong for a remote one, where a
// restart during a deploy on the other side would otherwise take this process
// down with it — so the number of attempts is configurable and the wait
// between them is fixed and short.
func pingRedis(client *redis.Client, config *viper.Viper, log *logrus.Logger) error {
	attempts := config.GetInt("redis.connect_attempts")
	if attempts < 1 {
		attempts = 1
	}

	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = client.Ping(ctx).Err()
		cancel()

		if err == nil {
			return nil
		}

		if attempt < attempts {
			log.Warnf("Redis is not answering (attempt %d/%d): %v", attempt, attempts, err)
			time.Sleep(2 * time.Second)
		}
	}

	return err
}

// duration reads a whole-second setting. Zero leaves go-redis on its own
// default rather than meaning "no timeout", which is what a zero in
// redis.Options would otherwise be taken for.
func duration(config *viper.Viper, key string) time.Duration {
	seconds := config.GetInt(key)
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
