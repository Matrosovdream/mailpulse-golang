package runner

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Plan says how much work to do and how much of it at once.
//
// Duration and Iterations are alternatives: whichever is set bounds the run.
// If both are set the run stops at the first of the two, which is what makes
// a duration a safety net on an iteration count that turns out to be slow.
type Plan struct {
	Concurrency int
	Duration    time.Duration
	Iterations  int
}

// Work is one unit of the thing under test. The index is unique across the
// whole run, so a suite can use it to pick a distinct row per iteration
// without coordinating between workers.
type Work func(ctx context.Context, index int) error

// Drive runs work at Plan.Concurrency until the plan is exhausted, timing each
// call, and returns the reduction.
//
// An error from work is counted, not fatal: a load test that stops at the
// first failure measures nothing about behaviour under failure, which is
// usually the interesting part.
func Drive(ctx context.Context, plan Plan, work Work) Stats {
	concurrency := plan.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}

	var (
		mu        sync.Mutex
		durations []time.Duration
		errors    int
		next      atomic.Int64
	)

	if plan.Iterations > 0 {
		durations = make([]time.Duration, 0, plan.Iterations)
	}

	runCtx := ctx
	var cancel context.CancelFunc
	if plan.Duration > 0 {
		runCtx, cancel = context.WithTimeout(ctx, plan.Duration)
		defer cancel()
	}

	started := time.Now()

	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				if runCtx.Err() != nil {
					return
				}

				index := int(next.Add(1)) - 1
				if plan.Iterations > 0 && index >= plan.Iterations {
					return
				}

				at := time.Now()
				err := work(runCtx, index)
				took := time.Since(at)

				// work that was cut short by the deadline is not a
				// measurement of anything: recording it would drag the tail
				// down with a truncated sample
				if runCtx.Err() != nil && err != nil {
					return
				}

				mu.Lock()
				durations = append(durations, took)
				if err != nil {
					errors++
				}
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	return Reduce(durations, errors, time.Since(started))
}
