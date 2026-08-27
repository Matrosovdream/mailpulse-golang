// Package suites defines what a load suite is and holds the registry the CLI
// dispatches through.
//
// The shape is the one this codebase already uses three times — event
// handlers, notifier channels, mail providers — so adding a fourth kind of
// test is a registration rather than a new switch statement.
package suites

import (
	"context"
	"flag"
	"fmt"
	"sort"

	"mailpulse/internal/loadtest/report"
	"mailpulse/internal/loadtest/runner"
)

// Suite is one kind of load test.
//
// Flags runs before Run, against a FlagSet owned by the subcommand, so each
// suite declares its own parameters without every other suite's flags
// crowding the help output.
type Suite interface {
	Name() string
	Synopsis() string
	NeedsDB() bool
	Flags(fs *flag.FlagSet)
	Run(ctx context.Context, env *runner.Env) (*report.Result, error)
}

type Registry struct {
	suites map[string]Suite
}

func NewRegistry() *Registry {
	return &Registry{suites: map[string]Suite{}}
}

func (r *Registry) Register(suites ...Suite) {
	for _, suite := range suites {
		r.suites[suite.Name()] = suite
	}
}

func (r *Registry) Get(name string) (Suite, error) {
	suite, ok := r.suites[name]
	if !ok {
		return nil, fmt.Errorf("no load suite named %q, try one of: %v", name, r.Names())
	}
	return suite, nil
}

func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.suites))
	for name := range r.suites {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *Registry) All() []Suite {
	all := make([]Suite, 0, len(r.suites))
	for _, name := range r.Names() {
		all = append(all, r.suites[name])
	}
	return all
}
