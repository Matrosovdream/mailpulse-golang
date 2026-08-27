// Package runner is the measurement layer every load suite shares: it times
// work, drives it at a chosen concurrency, and reduces the samples to the
// percentiles that actually decide whether a system is keeping up.
package runner

import (
	"math"
	"sort"
	"time"
)

// Stats is the reduction of a run's samples.
//
// The percentiles are the point. A mean hides the exact failure this system
// has: work is processed sequentially inside a cycle, so the tail sets
// capacity, not the average.
type Stats struct {
	Count     int           `json:"count"`
	Errors    int           `json:"errors"`
	Min       time.Duration `json:"min"`
	P50       time.Duration `json:"p50"`
	P95       time.Duration `json:"p95"`
	P99       time.Duration `json:"p99"`
	Max       time.Duration `json:"max"`
	Mean      time.Duration `json:"mean"`
	Elapsed   time.Duration `json:"elapsed"`
	PerSecond float64       `json:"per_second"`
}

// Percentile returns the value at p (0..1) using nearest-rank, which is the
// definition that always returns an observed sample rather than an
// interpolation between two of them.
func Percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}

	rank := int(math.Ceil(p*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}

	return sorted[rank]
}

// Reduce turns raw durations into Stats. elapsed is wall time for the whole
// run, which is what makes PerSecond a throughput rather than a reciprocal of
// the mean — the two differ by exactly the concurrency.
func Reduce(durations []time.Duration, errors int, elapsed time.Duration) Stats {
	stats := Stats{Count: len(durations), Errors: errors, Elapsed: elapsed}
	if len(durations) == 0 {
		return stats
	}

	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var total time.Duration
	for _, d := range sorted {
		total += d
	}

	stats.Min = sorted[0]
	stats.Max = sorted[len(sorted)-1]
	stats.P50 = Percentile(sorted, 0.50)
	stats.P95 = Percentile(sorted, 0.95)
	stats.P99 = Percentile(sorted, 0.99)
	stats.Mean = total / time.Duration(len(sorted))

	if elapsed > 0 {
		stats.PerSecond = float64(len(durations)) / elapsed.Seconds()
	}

	return stats
}
