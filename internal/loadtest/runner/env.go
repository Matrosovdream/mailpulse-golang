package runner

import (
	"math/rand"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

// Env is what cmd/loadtest hands every suite.
//
// DB is nil for suites that do not need one — the parser suite is pure CPU
// over a corpus, and requiring a database to run it would be a reason not to.
type Env struct {
	Log    *logrus.Logger
	Config *viper.Viper
	DB     *gorm.DB
	Seed   int64
}

// Rand returns a generator seeded from Env.Seed and the stream number, so two
// runs with the same -seed produce the same work and remain comparable, while
// two concurrent workers never share a generator.
func (e *Env) Rand(stream int) *rand.Rand {
	return rand.New(rand.NewSource(e.Seed + int64(stream)*7919))
}
