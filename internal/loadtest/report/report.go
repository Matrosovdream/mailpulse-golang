// Package report is how a run says what it found: an aligned table for the
// terminal and a JSON file for diffing two runs against each other.
//
// A load test whose only output is terminal scrollback cannot show a
// regression, which is why the JSON is not optional decoration.
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mailpulse/internal/loadtest/runner"
)

// Metric is one named block of timings, so a suite that measures several
// things (per endpoint, per corpus file) reports them side by side.
type Metric struct {
	Name  string         `json:"name"`
	Stats runner.Stats   `json:"stats"`
	Extra map[string]any `json:"extra,omitempty"`
}

// Result is one run, and the unit the JSON file holds.
type Result struct {
	Suite     string         `json:"suite"`
	StartedAt time.Time      `json:"started_at"`
	Elapsed   time.Duration  `json:"elapsed"`
	GitSHA    string         `json:"git_sha"`
	Params    map[string]any `json:"params"`
	Metrics   []Metric       `json:"metrics"`
	// Findings are the run's own conclusions in words — the derived number a
	// reader actually wants, like "sustains 900 accounts at a 30s interval".
	// A table of percentiles rarely says that by itself.
	Findings []string `json:"findings,omitempty"`
}

func (r *Result) Add(name string, stats runner.Stats, extra map[string]any) {
	r.Metrics = append(r.Metrics, Metric{Name: name, Stats: stats, Extra: extra})
}

func (r *Result) Findf(format string, args ...any) {
	r.Findings = append(r.Findings, fmt.Sprintf(format, args...))
}

// GitSHA records what was measured. Without it a results directory is a pile
// of numbers with no way back to the code that produced them.
func GitSHA() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// Table renders the result for a terminal.
func (r *Result) Table() string {
	var b strings.Builder

	fmt.Fprintf(&b, "\n%s — %s, %s\n", strings.ToUpper(r.Suite), r.GitSHA, r.Elapsed.Round(time.Millisecond))

	if len(r.Params) > 0 {
		keys := make([]string, 0, len(r.Params))
		for k := range r.Params {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s=%v", k, r.Params[k]))
		}
		fmt.Fprintf(&b, "%s\n", strings.Join(parts, "  "))
	}

	b.WriteString("\n")
	fmt.Fprintf(&b, "%-28s %7s %7s %10s %10s %10s %10s %9s\n",
		"metric", "n", "err", "p50", "p95", "p99", "max", "per sec")
	b.WriteString(strings.Repeat("-", 100) + "\n")

	for _, m := range r.Metrics {
		fmt.Fprintf(&b, "%-28s %7d %7d %10s %10s %10s %10s %9.1f\n",
			truncate(m.Name, 28), m.Stats.Count, m.Stats.Errors,
			round(m.Stats.P50), round(m.Stats.P95), round(m.Stats.P99), round(m.Stats.Max),
			m.Stats.PerSecond)

		for _, line := range extraLines(m.Extra) {
			fmt.Fprintf(&b, "%-28s   %s\n", "", line)
		}
	}

	if len(r.Findings) > 0 {
		b.WriteString("\n")
		for _, f := range r.Findings {
			fmt.Fprintf(&b, "  → %s\n", f)
		}
	}

	return b.String()
}

// Write saves the result as JSON under dir, named so runs sort chronologically
// and two suites never collide.
func (r *Result) Write(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	name := fmt.Sprintf("%s-%s.json", r.Suite, r.StartedAt.UTC().Format("20060102-150405"))
	path := filepath.Join(dir, name)

	encoded, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}

	return path, os.WriteFile(path, append(encoded, '\n'), 0o644)
}

func extraLines(extra map[string]any) []string {
	if len(extra) == 0 {
		return nil
	}

	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("%s: %v", k, extra[k]))
	}
	return lines
}

func round(d time.Duration) string {
	if d >= time.Second {
		return d.Round(10 * time.Millisecond).String()
	}
	return d.Round(time.Microsecond).String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
