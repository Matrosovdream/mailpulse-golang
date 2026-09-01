package usecase

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
)

// A zero value WorkerTuning has to mean "serial", which is the behaviour it
// replaced and the shape every direct caller of the usecases still gets.
func TestWorkerTuningLimitFloorsAtOne(t *testing.T) {
	cases := map[string]struct {
		tuning WorkerTuning
		want   int
	}{
		"zero value":  {WorkerTuning{}, 1},
		"explicit 0":  {WorkerTuning{Concurrency: 0}, 1},
		"negative":    {WorkerTuning{Concurrency: -8}, 1},
		"one":         {WorkerTuning{Concurrency: 1}, 1},
		"configured":  {WorkerTuning{Concurrency: 64}, 64},
		"unrealistic": {WorkerTuning{Concurrency: 100000}, 100000},
	}

	for name, testCase := range cases {
		if got := testCase.tuning.limit(); got != testCase.want {
			t.Errorf("%s: limit() = %d, want %d", name, got, testCase.want)
		}
	}
}

// The floor above is load bearing, not cosmetic.
//
// errgroup.SetLimit(0) permits no goroutines at all, so Go blocks and never
// returns. Passing an unfloored Concurrency straight through would mean
// WORKER_DISPATCH_CONCURRENCY=0 — the value an operator would reach for to turn
// pooling off — hanging the worker on its first non-empty batch instead, with
// the loop silent and the health probe reporting it stalled some minutes later.
func TestWorkerTuningZeroValueRunsItsBatch(t *testing.T) {
	group := new(errgroup.Group)
	group.SetLimit(WorkerTuning{}.limit())

	var handled atomic.Int64
	for range 4 {
		group.Go(func() error {
			handled.Add(1)
			return nil
		})
	}

	finished := make(chan struct{})
	go func() {
		_ = group.Wait()
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("a zero value tuning blocked its batch instead of working it serially")
	}

	if got := handled.Load(); got != 4 {
		t.Errorf("worked %d items of 4", got)
	}
}

func TestWorkerTuningItemDeadline(t *testing.T) {
	// Zero leaves an item unbounded, which is what the serial loops did.
	unbounded, cancelUnbounded := WorkerTuning{}.item(context.Background())
	defer cancelUnbounded()

	if _, ok := unbounded.Deadline(); ok {
		t.Error("a zero Timeout attached a deadline")
	}

	// A configured timeout has to actually bound the item: the failure it
	// exists for is a host that accepts the connection and then never answers,
	// which no amount of pooling recovers from on its own.
	bounded, cancelBounded := WorkerTuning{Timeout: 30 * time.Second}.item(context.Background())
	defer cancelBounded()

	deadline, ok := bounded.Deadline()
	if !ok {
		t.Fatal("a configured Timeout attached no deadline")
	}

	if remaining := time.Until(deadline); remaining <= 0 || remaining > 30*time.Second {
		t.Errorf("deadline is %s away, want just under 30s", remaining)
	}
}

// Cancelling the parent has to reach every item, bounded or not, so the
// worker's shutdown does not wait out a full batch of deadlines that have not
// come due. cmd/worker gives in-flight work five seconds after it cancels.
func TestWorkerTuningItemFollowsParentCancel(t *testing.T) {
	tunings := map[string]WorkerTuning{
		"unbounded": {},
		"bounded":   {Timeout: time.Hour},
	}

	parent, cancelParent := context.WithCancel(context.Background())

	items := make(map[string]context.Context, len(tunings))
	for name, tuning := range tunings {
		ctx, cancel := tuning.item(parent)
		defer cancel()

		if ctx.Err() != nil {
			t.Fatalf("%s: item context started already cancelled", name)
		}
		items[name] = ctx
	}

	cancelParent()

	for name, ctx := range items {
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
			t.Errorf("%s: item context outlived its parent's cancel", name)
		}
	}
}
