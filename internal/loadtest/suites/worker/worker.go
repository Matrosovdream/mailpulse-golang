// Package worker answers the question the whole load plan exists for: how many
// mail accounts can one worker keep on their poll interval, and what happens to
// the ones it cannot.
//
// It drives PipelineUseCase.PollDue directly rather than starting the worker
// binary and watching from outside. The loop is the thing under test, it
// already returns how much it did, and a harness that calls it is its own
// instrument — no metrics endpoint has to exist first.
package worker

import (
	"context"
	"flag"
	"fmt"
	"sync/atomic"
	"time"

	"mailpulse/internal/config"
	"mailpulse/internal/entity"
	"mailpulse/internal/gateway/mail/loadstub"
	"mailpulse/internal/gateway/secret"
	"mailpulse/internal/loadtest/fixtures"
	"mailpulse/internal/loadtest/report"
	"mailpulse/internal/loadtest/runner"

	"github.com/gofiber/fiber/v2"
)

type Suite struct {
	accounts    int
	watchers    int
	batch       int
	interval    time.Duration
	cycles      int
	workers     int
	p50         time.Duration
	p99         time.Duration
	timeout     time.Duration
	timeoutRate float64
	errorRate   float64
	messages    int
	matchRate   float64
	keep        bool
}

func New() *Suite { return &Suite{} }

func (s *Suite) Name() string  { return "worker" }
func (s *Suite) NeedsDB() bool { return true }
func (s *Suite) Synopsis() string {
	return "poller capacity: cycle time against the interval, and whether the queue drains"
}

func (s *Suite) Flags(fs *flag.FlagSet) {
	fs.IntVar(&s.accounts, "accounts", 200, "mail accounts to poll")
	fs.IntVar(&s.watchers, "watchers-per-account", 2, "watchers on each account")
	fs.IntVar(&s.batch, "batch", 0, "accounts claimed per cycle (default: worker.poll_batch)")
	fs.DurationVar(&s.interval, "interval", 0, "the interval a cycle must fit inside (default: worker.poll_interval)")
	fs.IntVar(&s.cycles, "cycles", 10, "poll cycles to run")
	fs.IntVar(&s.workers, "workers", 1, "concurrent pollers sharing the queue, for SKIP LOCKED contention")
	fs.DurationVar(&s.p50, "p50", 150*time.Millisecond, "median mailbox dial+fetch")
	fs.DurationVar(&s.p99, "p99", 2*time.Second, "99th percentile mailbox dial+fetch: this is what sets capacity")
	fs.DurationVar(&s.timeout, "timeout", 0, "what a hung mailbox costs (default: mail.imap_timeout)")
	fs.Float64Var(&s.timeoutRate, "timeout-rate", 0, "fraction of mailboxes that hang to the timeout")
	fs.Float64Var(&s.errorRate, "error-rate", 0, "fraction of mailboxes that fail transiently")
	fs.IntVar(&s.messages, "messages", 5, "messages synthesised per sync")
	fs.Float64Var(&s.matchRate, "match-rate", 0.2, "fraction of messages that match a watcher")
	fs.BoolVar(&s.keep, "keep", false, "leave the fixtures behind for inspection")
}

func (s *Suite) Run(ctx context.Context, env *runner.Env) (*report.Result, error) {
	batch := s.batch
	if batch <= 0 {
		batch = env.Config.GetInt("worker.poll_batch")
	}
	interval := s.interval
	if interval <= 0 {
		interval = time.Duration(env.Config.GetInt("worker.poll_interval")) * time.Second
	}
	timeout := s.timeout
	if timeout <= 0 {
		timeout = time.Duration(env.Config.GetInt("mail.imap_timeout")) * time.Second
	}

	cipher, err := secret.NewCipher(env.Config.GetString("security.encryption_key"))
	if err != nil {
		return nil, err
	}

	result := &report.Result{
		Suite:     s.Name(),
		StartedAt: time.Now(),
		GitSHA:    report.GitSHA(),
		Params: map[string]any{
			"accounts": s.accounts, "batch": batch, "interval": interval.String(),
			"cycles": s.cycles, "workers": s.workers,
			"p50": s.p50.String(), "p99": s.p99.String(),
			"timeout": timeout.String(), "timeout_rate": s.timeoutRate,
			"error_rate": s.errorRate, "messages": s.messages,
		},
	}

	env.Log.Infof("Building %d accounts", s.accounts)

	built, err := fixtures.Build(ctx, env.DB, cipher, fixtures.Shape{
		Users:              s.accounts,
		AccountsPerUser:    1,
		WatchersPerAccount: s.watchers,
		FiltersPerWatcher:  2,
		EventsPerWatcher:   1,
		Provider:           "imap",
		DueNow:             true,
	}, env.Seed)
	if err != nil {
		return nil, fmt.Errorf("building fixtures: %w", err)
	}

	if !s.keep {
		defer func() {
			if err := fixtures.Cleanup(context.WithoutCancel(ctx), env.DB); err != nil {
				env.Log.WithError(err).Error("Fixture cleanup failed; rows are still in the database")
			}
		}()
	}

	stub := loadstub.New(env.Log, loadstub.Config{
		P50: s.p50, P99: s.p99,
		Timeout: timeout, TimeoutRate: s.timeoutRate,
		ErrorRate:       s.errorRate,
		MessagesPerSync: s.messages,
		MatchRate:       s.matchRate,
	}, env.Seed)

	container := s.container(env)
	// swap the real IMAP client out: the registry is keyed by kind, and the
	// stub describes itself as KindIMAP, so this replaces it in place
	container.Providers.Register(stub)

	env.Log.Infof("Polling: %d cycles, batch %d, %d worker(s), interval %s",
		s.cycles, batch, s.workers, interval)

	// atomic because -workers runs several pollers against one queue, which is
	// the whole point of the contention experiment
	var polled atomic.Int64

	stats := runner.Drive(ctx, runner.Plan{
		Concurrency: s.workers,
		Iterations:  s.cycles,
	}, func(ctx context.Context, _ int) error {
		count, err := container.Pipeline.PollDue(ctx, batch)
		polled.Add(int64(count))
		return err
	})

	// queue depth is read after the run rather than sampled during it: the
	// number that matters is whether work is still waiting once the worker has
	// had its cycles, and sampling mid-run would race the claim transaction
	depth, err := s.depth(ctx, env)
	if err != nil {
		return nil, err
	}
	syncs, timeouts, failures := stub.Counts()

	result.Add("poll-cycle", stats, map[string]any{
		"accounts_polled": polled.Load(),
		"queue_depth_end": depth,
		"stub_syncs":      syncs,
		"stub_timeouts":   timeouts,
		"stub_failures":   failures,
	})

	result.Elapsed = time.Since(result.StartedAt)
	s.conclude(result, stats, built.Accounts(), batch, interval, depth, int(polled.Load()))

	return result, nil
}

// depth counts the accounts still due. It is the one signal that cannot be
// gamed by a flattering throughput number: a worker that is falling behind
// still reports plenty of syncs per second, but its queue climbs.
func (s *Suite) depth(ctx context.Context, env *runner.Env) (int64, error) {
	var due int64
	err := env.DB.WithContext(ctx).Model(&entity.MailAccount{}).
		Where("email_address LIKE ?", "%@"+fixtures.EmailDomain).
		Where("next_poll_at <= ?", time.Now().UnixMilli()).
		Where("status = ?", entity.MailAccountStatusVerified).
		Count(&due).Error

	return due, err
}

// container wires the real application the worker binary runs, so what is
// measured is the production path with one substituted mailbox client.
func (s *Suite) container(env *runner.Env) *config.Container {
	return config.Bootstrap(&config.BootstrapConfig{
		DB:       env.DB,
		App:      fiber.New(),
		Log:      env.Log,
		Validate: config.NewValidator(env.Config),
		Config:   env.Config,
		Producer: nil,
		Redis:    config.NewRedis(env.Config, env.Log),
	})
}

// conclude states the capacity answer in a sentence, because the interesting
// number here is derived rather than measured: cycle time against the interval
// is what says how many accounts fit.
func (s *Suite) conclude(result *report.Result, stats runner.Stats, accounts, batch int,
	interval time.Duration, depth int64, polled int) {

	if stats.Count == 0 {
		result.Findf("no cycles ran")
		return
	}

	result.Findf("cycle p50 %s, p99 %s, against a %s interval",
		stats.P50.Round(time.Millisecond), stats.P99.Round(time.Millisecond), interval)

	if stats.P99 > interval {
		result.Findf("OVER BUDGET: the p99 cycle exceeds the interval, so ticks are being dropped and the worker is falling behind")
	}

	// capacity is batch-per-cycle scaled by how many cycles fit in an interval
	if stats.P50 > 0 {
		cyclesPerInterval := interval.Seconds() / stats.P50.Seconds()
		capacity := int(cyclesPerInterval * float64(batch))
		result.Findf("sustains roughly %d accounts at a %s interval (batch %d, %.1f cycles per interval)",
			capacity, interval, batch, cyclesPerInterval)

		if accounts > capacity {
			result.Findf("the %d accounts under test are already past that ceiling", accounts)
		}
	}

	result.Findf("polled %d accounts across %d cycles; %d still due at the end", polled, stats.Count, depth)

	if depth > 0 && polled > 0 {
		result.Findf("queue depth is non-zero after the run: raise -cycles to see whether it drains or climbs")
	}
}
