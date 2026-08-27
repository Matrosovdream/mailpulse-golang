// Package parser measures MIME decoding over a corpus of real message shapes.
//
// This is bones on purpose: the corpus is small and the harness reports
// throughput per shape and the slowest individual message. What it is for is
// finding pathological inputs — the nested multipart or the large attachment
// that costs a hundred times what a plain text mail does — because sync time
// is per message and one bad shape in a mailbox is paid on every poll.
//
// It is not a regression gate. That job belongs to a testing.B beside the
// code, where benchstat can compare two runs on the same hardware.
package parser

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mailpulse/internal/gateway/mail/imap"
	"mailpulse/internal/loadtest/report"
	"mailpulse/internal/loadtest/runner"
)

//go:embed corpus/*.eml
var builtin embed.FS

type Suite struct {
	corpus      string
	iterations  int
	concurrency int
	only        string
	verbose     bool
}

func New() *Suite { return &Suite{} }

func (s *Suite) Name() string  { return "parser" }
func (s *Suite) NeedsDB() bool { return false }
func (s *Suite) Synopsis() string {
	return "MIME parse throughput per message shape, and which shapes are pathological"
}

func (s *Suite) Flags(fs *flag.FlagSet) {
	fs.StringVar(&s.corpus, "corpus", "", "directory of .eml files to use instead of the built-in corpus")
	fs.IntVar(&s.iterations, "iterations", 2000, "parses per corpus message")
	fs.IntVar(&s.concurrency, "concurrency", 1, "parallel parsers; 1 measures cost, more measures scaling")
	fs.StringVar(&s.only, "message", "", "run a single corpus message by name")
	fs.BoolVar(&s.verbose, "show", false, "print what each message parsed into, as a sanity check")
}

type sample struct {
	name string
	raw  []byte
}

func (s *Suite) Run(ctx context.Context, env *runner.Env) (*report.Result, error) {
	corpus, source, err := s.load()
	if err != nil {
		return nil, err
	}
	if len(corpus) == 0 {
		return nil, fmt.Errorf("no .eml files found in %s", source)
	}

	result := &report.Result{
		Suite:     s.Name(),
		StartedAt: time.Now(),
		GitSHA:    report.GitSHA(),
		Params: map[string]any{
			"corpus": source, "messages": len(corpus),
			"iterations": s.iterations, "concurrency": s.concurrency,
		},
	}

	for _, message := range corpus {
		if s.only != "" && s.only != message.name {
			continue
		}

		// parse once outside the timing loop, both to surface a corpus file
		// that does not parse at all and to report what the shape produced
		parsed, parseErr := imap.ParseMessage(env.Log, message.raw)
		if parseErr != nil {
			env.Log.WithError(parseErr).Warnf("%s does not parse; measuring the failure path", message.name)
		}
		if s.verbose {
			env.Log.Infof("%s -> subject=%q from=%q body=%dB attachments=%v",
				message.name, parsed.Subject, parsed.FromAddress,
				len(parsed.BodyText), parsed.AttachmentNames)
		}

		raw := message.raw
		stats := runner.Drive(ctx, runner.Plan{
			Concurrency: s.concurrency,
			Iterations:  s.iterations,
		}, func(ctx context.Context, _ int) error {
			_, err := imap.ParseMessage(env.Log, raw)
			return err
		})

		extra := map[string]any{
			"bytes":       len(raw),
			"mb_per_sec":  megabytesPerSecond(len(raw), stats),
			"body_bytes":  len(parsed.BodyText),
			"attachments": len(parsed.AttachmentNames),
		}
		if parseErr != nil {
			extra["parse_error"] = parseErr.Error()
		}

		result.Add(message.name, stats, extra)
	}

	result.Elapsed = time.Since(result.StartedAt)
	s.conclude(result)

	return result, nil
}

// load prefers a directory the caller pointed at, and falls back to the corpus
// compiled into the binary so the suite runs with no arguments at all.
func (s *Suite) load() ([]sample, string, error) {
	if s.corpus != "" {
		entries, err := os.ReadDir(s.corpus)
		if err != nil {
			return nil, s.corpus, err
		}

		var corpus []sample
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".eml") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(s.corpus, entry.Name()))
			if err != nil {
				return nil, s.corpus, err
			}
			corpus = append(corpus, sample{name: entry.Name(), raw: raw})
		}
		return corpus, s.corpus, nil
	}

	entries, err := builtin.ReadDir("corpus")
	if err != nil {
		return nil, "builtin", err
	}

	var corpus []sample
	for _, entry := range entries {
		raw, err := builtin.ReadFile("corpus/" + entry.Name())
		if err != nil {
			return nil, "builtin", err
		}
		corpus = append(corpus, sample{name: entry.Name(), raw: raw})
	}

	sort.Slice(corpus, func(i, j int) bool { return corpus[i].name < corpus[j].name })

	return corpus, "builtin", nil
}

func megabytesPerSecond(size int, stats runner.Stats) string {
	if stats.Elapsed <= 0 {
		return "0"
	}
	total := float64(size) * float64(stats.Count)
	return fmt.Sprintf("%.1f", total/stats.Elapsed.Seconds()/(1<<20))
}

// conclude names the outlier, which is the finding this suite exists for.
func (s *Suite) conclude(result *report.Result) {
	if len(result.Metrics) < 2 {
		return
	}

	cheapest, dearest := result.Metrics[0], result.Metrics[0]
	for _, m := range result.Metrics {
		if m.Stats.P50 < cheapest.Stats.P50 {
			cheapest = m
		}
		if m.Stats.P50 > dearest.Stats.P50 {
			dearest = m
		}
	}

	result.Findf("cheapest shape: %s at %s per parse", cheapest.Name, cheapest.Stats.P50)
	result.Findf("dearest shape: %s at %s per parse", dearest.Name, dearest.Stats.P50)

	if cheapest.Stats.P50 > 0 {
		ratio := float64(dearest.Stats.P50) / float64(cheapest.Stats.P50)
		result.Findf("%.0fx spread between them: sync cost is per message, so a mailbox full of the dear shape polls %.0fx slower",
			ratio, ratio)
	}
}
