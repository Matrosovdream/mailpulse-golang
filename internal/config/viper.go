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

	config.SetDefault("web.port", 3000)
	config.SetDefault("web.prefork", false)

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

	config.SetDefault("kafka.bootstrap.servers", "localhost:9092")
	config.SetDefault("kafka.group.id", "mailpulse")
	config.SetDefault("kafka.auto.offset.reset", "earliest")
	config.SetDefault("kafka.producer.enabled", false)
}
