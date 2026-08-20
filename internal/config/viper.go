package config

import (
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

// NewViper builds the configuration entirely from environment variables.
//
// Keys map by upper-casing and replacing dots with underscores, so
// database.host reads DATABASE_HOST and redis.ttl.auth reads REDIS_TTL_AUTH.
//
// Resolution order is environment, then the defaults below. A .env file is
// loaded into the environment first when one exists, so the same file
// configures a container (via env_file) and a binary run straight from the
// shell. Real environment variables always win over the file, which is how
// production supplies secrets without a file on disk.
func NewViper() *viper.Viper {
	// godotenv never overwrites a variable that is already set.
	// ../.env covers packages that run from a subdirectory, such as ./test.
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../.env")

	config := viper.New()

	config.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	config.AutomaticEnv()

	// treat FOO= as an explicit empty value rather than falling back to the
	// default, so an intentionally blank REDIS_PASSWORD stays blank
	config.AllowEmptyEnv(true)

	setDefaults(config)

	return config
}

// setDefaults keeps the application startable with no environment at all:
// values safe for running the binaries against services on localhost.
// Anything secret or environment-specific belongs in .env, not here.
func setDefaults(config *viper.Viper) {
	config.SetDefault("app.name", "mailpulse")
	config.SetDefault("app.version", "1.0.0")
	config.SetDefault("app.base_url", "http://localhost:3000")

	config.SetDefault("web.port", 3000)
	config.SetDefault("web.prefork", false)
	// CSV of proxy addresses or CIDR ranges allowed to set X-Forwarded-For.
	// Empty means trust nothing and read the peer address, which is the safe
	// default: trusting the header unconditionally lets any caller claim any IP
	// and defeats both the rate limiter and the audit trail.
	config.SetDefault("web.trusted_proxies", "")
	// CSV of browser origins allowed to read responses, e.g.
	// http://localhost:5173. Empty means no CORS middleware is mounted at all,
	// which is the safe default: fiber reads an empty allowlist as "*", so the
	// choice is deliberately between named origins and none, never a wildcard.
	config.SetDefault("web.cors_origins", "")
	// serves GET /api/docs and GET /api/openapi.yaml, both unauthenticated.
	// On by default because the description is how the API is meant to be read;
	// turn it off where naming every endpoint to an anonymous caller is not
	// wanted, and the two routes are not registered at all.
	config.SetDefault("web.docs_enabled", true)

	// there is deliberately no default encryption key: a shipped one reads as
	// protection while providing none, so an unset key stops the app instead
	config.SetDefault("security.encryption_key", "")

	// Both counters are per IP per window, and the middleware cannot tell a
	// failed login from a successful one, so the allowance has to cover a whole
	// office behind one NAT address rather than a single person. It is set to
	// make online guessing impractical, not to stop it dead; a per-account
	// counter on failures only is the stronger control and is a separate change.
	config.SetDefault("security.ratelimit.login.attempts", 30)
	config.SetDefault("security.ratelimit.login.window", 300) // 5 minutes
	// tighter, because each attempt sends mail: this is an abuse cost as much
	// as a guessing brake
	config.SetDefault("security.ratelimit.forgot_password.attempts", 10)
	config.SetDefault("security.ratelimit.forgot_password.window", 900) // 15 minutes

	config.SetDefault("session.ttl", 604800) // 7 days

	config.SetDefault("worker.poll_interval", 30)
	config.SetDefault("worker.poll_batch", 20)
	config.SetDefault("worker.dispatch_interval", 5)
	config.SetDefault("worker.dispatch_batch", 50)
	config.SetDefault("worker.verify_interval", 3600)
	config.SetDefault("worker.verify_batch", 20)

	config.SetDefault("mail.imap_timeout", 30)
	// how stale a credential check may get before the worker re-tests it
	config.SetDefault("mail.reverify_after", 21600) // 6 hours
	// development only: fake the mailbox instead of connecting to one
	config.SetDefault("mail.stub_enabled", false)

	config.SetDefault("telegram.bot_token", "")

	// the mail server the feature tests connect to; the dev stack runs one
	config.SetDefault("test.mail_host", "greenmail")
	config.SetDefault("test.mail_port", 3143)
	config.SetDefault("test.smtp_port", 3025)
	config.SetDefault("test.mail_username", "demo")
	config.SetDefault("test.mail_password", "secret123")
	config.SetDefault("test.mail_address", "demo@corp.com")

	// logrus levels: 4=warn 5=info 6=debug
	config.SetDefault("log.level", 6)

	config.SetDefault("database.host", "localhost")
	config.SetDefault("database.port", 5432)
	config.SetDefault("database.username", "postgres")
	config.SetDefault("database.password", "postgres")
	config.SetDefault("database.name", "mailpulse")
	config.SetDefault("database.sslmode", "disable")
	config.SetDefault("database.timezone", "UTC")
	config.SetDefault("database.pool.idle", 10)
	config.SetDefault("database.pool.max", 100)
	config.SetDefault("database.pool.lifetime", 300)

	config.SetDefault("redis.host", "localhost")
	config.SetDefault("redis.port", 6379)
	config.SetDefault("redis.password", "")
	config.SetDefault("redis.db", 0)
	config.SetDefault("redis.pool.size", 10)
	config.SetDefault("redis.ttl.auth", 300)
	config.SetDefault("redis.ttl.password_reset", 1800)

	config.SetDefault("kafka.bootstrap.servers", "localhost:9092")
	config.SetDefault("kafka.group.id", "mailpulse")
	config.SetDefault("kafka.auto.offset.reset", "earliest")
	config.SetDefault("kafka.producer.enabled", false)
}
