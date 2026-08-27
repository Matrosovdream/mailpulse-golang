// Package guard stands between a load run and someone's real data.
//
// Every database-backed suite inserts thousands of rows and then deletes them
// again. Pointed at the wrong database that is not a slow test, it is data
// loss, and the flags that would cause it (a stale .env, a copied command
// line) are exactly the ones nobody inspects before pressing enter.
package guard

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// OverrideEnv names the database the caller is knowingly targeting. It must
// match exactly: a boolean "yes I am sure" would be set once and then forgotten
// in a shell profile, where it protects nobody.
const OverrideEnv = "LOADTEST_DATABASE"

// known is the set of databases a load run may touch without being told twice.
var known = map[string]bool{
	"mailpulse":      true,
	"mailpulse_dev":  true,
	"mailpulse_test": true,
	"mailpulse_load": true,
}

// DevDatabase refuses to proceed against anything that is not recognisably a
// development database, and names the escape hatch rather than leaving the
// caller to find it.
func DevDatabase(config *viper.Viper) error {
	name := config.GetString("database.name")
	host := config.GetString("database.host")

	if override := os.Getenv(OverrideEnv); override != "" {
		if override != name {
			return fmt.Errorf(
				"%s is set to %q but the configured database is %q: refusing to guess which one you meant",
				OverrideEnv, override, name)
		}
		return nil
	}

	if !known[name] {
		return fmt.Errorf(
			"database %q on %s is not a recognised development database.\n"+
				"A load run inserts and deletes thousands of rows.\n"+
				"If this really is the target, set %s=%s",
			name, host, OverrideEnv, name)
	}

	return nil
}

// Describe is what the CLI prints before it starts, so the target is on screen
// rather than implied by whichever .env happened to load.
func Describe(config *viper.Viper) string {
	return fmt.Sprintf("%s@%s:%d/%s",
		config.GetString("database.username"),
		config.GetString("database.host"),
		config.GetInt("database.port"),
		strings.TrimSpace(config.GetString("database.name")))
}
