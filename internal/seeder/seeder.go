// Package seeder loads development and bootstrap data from JSON files, one
// file per table.
//
// It is deliberately separate from migrations: migrations own the shape of the
// schema and must be identical everywhere, while seeds are data you may want in
// development, in a fresh production install, or not at all. Seeders are
// idempotent, so running them twice is safe.
package seeder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// Result is what one seeder did, so the command can print a useful summary
// rather than just "ok".
type Result struct {
	Created int
	Updated int
	Skipped int
}

func (r Result) String() string {
	return fmt.Sprintf("%d created, %d updated, %d unchanged", r.Created, r.Updated, r.Skipped)
}

// Seeder loads one table. Name doubles as the JSON filename: a seeder called
// "users" reads db/seeds/users.json.
//
// Adding a table means writing one of these and registering it in Default().
type Seeder interface {
	Name() string
	Seed(ctx context.Context, db *gorm.DB, raw json.RawMessage) (Result, error)
}

// Registry keeps seeders in the order they must run, because rows reference
// each other: users before the mail accounts that belong to them.
type Registry struct {
	seeders []Seeder
	Log     *logrus.Logger
}

func NewRegistry(log *logrus.Logger) *Registry {
	return &Registry{Log: log}
}

func (r *Registry) Register(seeders ...Seeder) {
	r.seeders = append(r.seeders, seeders...)
}

func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.seeders))
	for _, seeder := range r.seeders {
		names = append(names, seeder.Name())
	}
	return names
}

// ErrNoFile means a seeder is registered but has no JSON file, which is not an
// error: it lets an environment opt out of a seed by deleting its file.
var ErrNoFile = errors.New("no seed file")

// Run executes every registered seeder, or just one when only is set.
func (r *Registry) Run(ctx context.Context, db *gorm.DB, dir string, only string) error {
	matched := false

	for _, seeder := range r.seeders {
		if only != "" && seeder.Name() != only {
			continue
		}
		matched = true

		raw, err := r.load(dir, seeder.Name())
		if errors.Is(err, ErrNoFile) {
			r.Log.Infof("%-16s skipped, %s.json is not present", seeder.Name(), seeder.Name())
			continue
		}
		if err != nil {
			return fmt.Errorf("%s: %w", seeder.Name(), err)
		}

		// each seeder runs in its own transaction: a bad file fails that
		// table alone rather than half-applying every table
		var result Result
		err = db.Transaction(func(tx *gorm.DB) error {
			var txErr error
			result, txErr = seeder.Seed(ctx, tx, raw)
			return txErr
		})
		if err != nil {
			return fmt.Errorf("%s: %w", seeder.Name(), err)
		}

		r.Log.Infof("%-16s %s", seeder.Name(), result)
	}

	if only != "" && !matched {
		return fmt.Errorf("no seeder called %q, known seeders: %v", only, r.Names())
	}

	return nil
}

func (r *Registry) load(dir, name string) (json.RawMessage, error) {
	path := filepath.Join(dir, name+".json")

	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoFile
	}
	if err != nil {
		return nil, err
	}

	// fail on a malformed file here, with the path in the message, rather than
	// somewhere inside a seeder
	if !json.Valid(raw) {
		return nil, fmt.Errorf("%s is not valid JSON", path)
	}

	return raw, nil
}

// Default is the ordered set of seeders the command runs.
//
// Order matters: a table whose rows reference another must come after it.
func Default(log *logrus.Logger) *Registry {
	registry := NewRegistry(log)
	registry.Register(
		NewMailProviderSeeder(),
		NewUserSeeder(),
	)
	return registry
}
