package usecase

import (
	"fmt"
	"strings"
	"testing"

	"mailpulse/internal/entity"
	"mailpulse/internal/gateway/mail"
)

// The matcher is the product's hot inner loop: it runs once per message per
// watcher per filter, so its cost is multiplied by three separate fan-outs
// before it reaches a poll cycle's budget. These benchmarks exist to be
// compared against each other with benchstat, not read as absolute figures.

func benchWatcher(mode string) *entity.Watcher {
	return &entity.Watcher{MatchMode: mode}
}

func benchFilters(n int) []entity.WatcherFilter {
	fields := []string{entity.FieldSubject, entity.FieldFrom, entity.FieldTo, entity.FieldBody}
	filters := make([]entity.WatcherFilter, 0, n)

	for i := 0; i < n; i++ {
		filters = append(filters, entity.WatcherFilter{
			Field:    fields[i%len(fields)],
			Operator: entity.OpContains,
			Value:    fmt.Sprintf("term-%d", i),
			Position: i,
		})
	}

	return filters
}

func benchMessage(bodyBytes int) *mail.Message {
	return &mail.Message{
		Subject:     "Invoice 4471 is ready for payment",
		FromAddress: "billing@example.com",
		FromName:    "Billing",
		To:          []string{"recipient@example.com"},
		BodyText:    strings.Repeat("body text for the matcher. ", bodyBytes/27+1),
		Headers:     map[string]string{"X-Mailer": "bench", "List-Id": "<alerts.example.com>"},
		SizeBytes:   bodyBytes,
	}
}

// BenchmarkEvaluateFilters_Count sweeps the filter count, which is the fan-out
// a user controls directly through the UI.
func BenchmarkEvaluateFilters_Count(b *testing.B) {
	message := benchMessage(2048)

	for _, count := range []int{1, 4, 16, 64} {
		filters := benchFilters(count)
		watcher := benchWatcher(entity.MatchModeAll)

		b.Run(fmt.Sprintf("filters=%d", count), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = EvaluateFilters(watcher, filters, message)
			}
		})
	}
}

// BenchmarkEvaluateFilters_BodySize sweeps the body, because a contains over
// the body is the one operator whose cost grows with the mail rather than with
// the filter list.
func BenchmarkEvaluateFilters_BodySize(b *testing.B) {
	filters := []entity.WatcherFilter{{
		Field: entity.FieldBody, Operator: entity.OpContains, Value: "needle-not-present",
	}}
	watcher := benchWatcher(entity.MatchModeAll)

	for _, size := range []int{1 << 10, 1 << 14, 1 << 18} {
		message := benchMessage(size)

		b.Run(fmt.Sprintf("body=%dB", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				_ = EvaluateFilters(watcher, filters, message)
			}
		})
	}
}

// BenchmarkEvaluateFilters_Operators compares the operator set. regex is the
// one worth watching: it is the only operator that can be made arbitrarily
// expensive by a user typing into a form.
func BenchmarkEvaluateFilters_Operators(b *testing.B) {
	message := benchMessage(2048)
	watcher := benchWatcher(entity.MatchModeAll)

	operators := []struct {
		name  string
		value string
		op    string
	}{
		{"contains", "Invoice", entity.OpContains},
		{"equals", "Invoice 4471 is ready for payment", entity.OpEquals},
		{"starts_with", "Invoice", entity.OpStartsWith},
		{"ends_with", "payment", entity.OpEndsWith},
		{"regex", `^Invoice \d+ is ready`, entity.OpRegex},
	}

	for _, operator := range operators {
		filters := []entity.WatcherFilter{{
			Field: entity.FieldSubject, Operator: operator.op, Value: operator.value,
		}}

		b.Run(operator.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = EvaluateFilters(watcher, filters, message)
			}
		})
	}
}

// BenchmarkEvaluateFilters_MatchMode checks that "any" short-circuits on the
// first pass and "all" on the first failure, by ordering the filters so each
// mode meets its exit condition last.
func BenchmarkEvaluateFilters_MatchMode(b *testing.B) {
	message := benchMessage(2048)
	filters := benchFilters(16)

	for _, mode := range []string{entity.MatchModeAll, entity.MatchModeAny} {
		watcher := benchWatcher(mode)

		b.Run(mode, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = EvaluateFilters(watcher, filters, message)
			}
		})
	}
}
