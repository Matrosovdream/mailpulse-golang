// Command loadtest runs MailPulse's load suites.
//
// Each suite is a subcommand with its own flags, because the questions are
// genuinely different: "does this endpoint hold up at 50 concurrent callers"
// and "how many mailboxes can one worker keep on their interval" do not share
// a parameter list. What they do share is the timing, reporting and fixture
// machinery in internal/loadtest.
//
//	loadtest                         list the suites
//	loadtest parser                  no database needed
//	loadtest worker -accounts=2000   capacity, against the load stub
//	loadtest endpoints -concurrency=50
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"mailpulse/internal/config"
	"mailpulse/internal/loadtest/fixtures"
	"mailpulse/internal/loadtest/guard"
	"mailpulse/internal/loadtest/runner"
	"mailpulse/internal/loadtest/suites"
	"mailpulse/internal/loadtest/suites/endpoints"
	"mailpulse/internal/loadtest/suites/parser"
	"mailpulse/internal/loadtest/suites/worker"

	"github.com/sirupsen/logrus"
)

const resultsDir = "test/load/results"

func main() {
	registry := suites.NewRegistry()
	registry.Register(
		endpoints.New(),
		parser.New(),
		worker.New(),
	)

	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help" {
		usage(registry)
		return
	}

	name := os.Args[1]

	// clean is not a suite: it produces no measurement. It exists because a
	// run killed part-way through — Ctrl-C, a panic, a failed cleanup — leaves
	// its rows behind, and the next run should not have to inherit them.
	if name == "clean" {
		clean()
		return
	}

	suite, err := registry.Get(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n\n", err)
		usage(registry)
		os.Exit(2)
	}

	fs := flag.NewFlagSet(name, flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "loadtest %s — %s\n\n", suite.Name(), suite.Synopsis())
		fs.PrintDefaults()
	}

	// flags every suite honours, declared here so they cannot drift apart
	seed := fs.Int64("seed", 1, "RNG seed; the same seed builds the same world, which is what makes two runs comparable")
	jsonOut := fs.Bool("json", true, "write the result to "+resultsDir)
	quiet := fs.Bool("quiet", false, "suppress the application's own log lines, leaving only the report")

	suite.Flags(fs)

	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}

	viperConfig := config.NewViper()
	log := config.NewLogger(viperConfig)
	if *quiet {
		log.SetLevel(logrus.ErrorLevel)
	}

	env := &runner.Env{Log: log, Config: viperConfig, Seed: *seed}

	if suite.NeedsDB() {
		if err := guard.DevDatabase(viperConfig); err != nil {
			fmt.Fprintf(os.Stderr, "\nrefusing to run:\n%v\n\n", err)
			os.Exit(1)
		}
		log.Infof("Load target: %s", guard.Describe(viperConfig))
		env.DB = config.NewDatabase(viperConfig, log)
	}

	// Ctrl-C has to reach the suite rather than the process, so a run that is
	// interrupted still cleans its fixtures up on the way out
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	result, err := suite.Run(ctx, env)
	if err != nil {
		log.Errorf("%s failed: %v", name, err)
		os.Exit(1)
	}

	fmt.Print(result.Table())

	if *jsonOut {
		path, err := result.Write(resultsDir)
		if err != nil {
			log.WithError(err).Warn("Could not write the result file")
		} else {
			fmt.Printf("\nresult: %s\n", path)
		}
	}
}

func usage(registry *suites.Registry) {
	fmt.Fprintf(os.Stderr, "usage: loadtest <suite> [flags]\n\nsuites:\n")

	for _, suite := range registry.All() {
		needs := ""
		if suite.NeedsDB() {
			needs = "  (needs a database)"
		}
		fmt.Fprintf(os.Stderr, "  %-12s %s%s\n", suite.Name(), suite.Synopsis(), needs)
	}

	fmt.Fprintf(os.Stderr, "  %-12s %s\n", "clean", "delete rows a previous run left behind")

	fmt.Fprintf(os.Stderr, "\nrun 'loadtest <suite> -h' for that suite's flags\n")
	fmt.Fprintf(os.Stderr, "results are written to %s\n", resultsDir)
	fmt.Fprintf(os.Stderr, "\nsuites that touch the database refuse to run outside a known dev database;\n")
	fmt.Fprintf(os.Stderr, "override with %s=<name>\n", guard.OverrideEnv)
}

// clean removes whatever a previous run left in the database, scoped to the
// load domain so it can never take a developer's own rows with it.
func clean() {
	viperConfig := config.NewViper()
	log := config.NewLogger(viperConfig)

	if err := guard.DevDatabase(viperConfig); err != nil {
		fmt.Fprintf(os.Stderr, "\nrefusing to run:\n%v\n\n", err)
		os.Exit(1)
	}

	db := config.NewDatabase(viperConfig, log)
	ctx := context.Background()

	users, accounts, err := fixtures.Count(ctx, db)
	if err != nil {
		log.Errorf("Could not count load rows: %v", err)
		os.Exit(1)
	}

	if users == 0 && accounts == 0 {
		fmt.Printf("nothing to clean in %s\n", guard.Describe(viperConfig))
		return
	}

	fmt.Printf("removing %d users and %d mail accounts from %s\n",
		users, accounts, guard.Describe(viperConfig))

	if err := fixtures.Cleanup(ctx, db); err != nil {
		log.Errorf("Cleanup failed: %v", err)
		os.Exit(1)
	}

	fmt.Println("clean")
}
