package usecase

import (
	"context"
	"time"
)

// WorkerTuning bounds how one process works through a batch it has claimed.
//
// The claim itself is already safe to run from several processes — both queues
// use SKIP LOCKED — so nothing here is about coordination between workers. It
// is only about what a single worker does with the rows it won.
//
// That turns out to be the tightest constraint in the whole worker. Every item
// in both loops spends nearly all of its wall time waiting on a remote host: a
// notification provider for the dispatcher, an IMAP server for the poller.
// Worked serially, a batch takes items × latency no matter how it was sized,
// so throughput settles at one over the per-item latency and neither the batch
// size nor the tick interval can move it. Concurrency is the only term in that
// expression a caller can raise.
type WorkerTuning struct {
	// Concurrency is how many items of one batch are in flight at once.
	// Anything below 1 means 1, which is the serial behaviour this replaces.
	//
	// It is bounded rather than unlimited because the limit protects resources
	// this process does not own. One goroutine per claimed row would take a
	// database connection each and could drain database.pool.max on a single
	// tick, and on the poll side would open that many IMAP connections at once
	// — which is how a worker gets itself rate limited by a mail provider.
	Concurrency int

	// Timeout bounds a single item. Zero leaves it unbounded, which is the
	// behaviour this replaces.
	//
	// A pool makes a deadline more necessary, not less. Serially, one mailbox
	// that accepts a connection and then never answers stalls the loop
	// outright, and the health probe in cmd/worker reports the silence. With a
	// pool the same mailbox quietly holds one slot while the loop keeps
	// ticking, so the worker runs at reduced capacity and reports itself
	// healthy — until enough slots are held to stop it, at which point the
	// cause is hours old.
	Timeout time.Duration
}

// limit is Concurrency floored at 1, so a zero value WorkerTuning is exactly
// the serial behaviour it replaces.
func (t WorkerTuning) limit() int {
	if t.Concurrency < 1 {
		return 1
	}
	return t.Concurrency
}

// item derives the context for a single item of a batch.
//
// The returned cancel must always be called, including when Timeout is zero
// and no deadline was attached — the context is still derived from the
// caller's and holds a goroutine until it is released.
func (t WorkerTuning) item(ctx context.Context) (context.Context, context.CancelFunc) {
	if t.Timeout <= 0 {
		return context.WithCancel(ctx)
	}

	return context.WithTimeout(ctx, t.Timeout)
}
