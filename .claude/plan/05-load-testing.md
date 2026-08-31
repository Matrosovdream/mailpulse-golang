# 05 — Load and performance testing

**Status:** running. `cmd/loadtest` exists with three suites — `endpoints`,
`worker`, `parser` — plus benchmarks beside the code, and the first
measurements are in. Two of the arithmetic figures below have been replaced by
numbers, and both were worse than the arithmetic suggested.

**Measured 2026-08-27** on the dev stack (relative figures, not capacity):

- **The poller sustains ~226 accounts at a 30s interval** with a healthy
  mailbox distribution (p50 150ms, p99 2s, batch 20).
- **One mailbox in twenty hanging to `MAIL_IMAP_TIMEOUT` collapses that to
  ~19** — a 12x drop, with p99 cycle time at 1m36s against a 30s interval. This
  is §6's "one hung mailbox" experiment and it is the sharpest result here:
  capacity is set by the tail, not the mean, which is the argument for
  per-item deadlines rather than more workers.
- **`GET /api/admin/users` was ~136x slower than its neighbours** — 96ms p50
  against 711µs for `/api/matches` at the same page size. The N+1 in §5 was
  real and it was the single worst endpoint. **Fixed 2026-08-28**: the four
  per-row counts became four grouped counts per page via `countByUsers`.
  Measured on the same scenario, 500 users at `size=100`: **98.6ms -> 7.8ms
  p50, and 96 -> 1,209 req/s. 12.6x.**
- **A worse N+1 was found in the poll loop, and fixed 2026-08-28.**
  `Filters.FindByWatcher` sat inside `evaluate`, which runs per message inside
  a loop over watchers — `messages x watchers` queries per sync, up to 600 for
  a full fetch against three watchers, on every poll of every account forever.
  The watchers were already loaded once per sync; the filters now are too, via
  the `FindByWatchers` that already existed. **Measured: cycle p50 201ms ->
  63ms at 50 messages x 3 watchers, and projected capacity 2,987 -> 9,530
  accounts.** Caching would have been the wrong fix; the query was.
- **`EvaluateFilters` recompiles the regex on every call** — `regexp.Compile`
  at [matcher.go:125](../../internal/usecase/matcher.go#L125), inside a
  function that runs per message per watcher per filter. 1938ns and 35 allocs
  against 275ns and 7 for `contains`.
- **AES-GCM is not a problem**: 145ns to decrypt a small credential blob, so
  the ~83/sec sustained at 10k accounts is noise.

Metrics and pprof (§2) are still absent, and were **not** needed to get here:
see the note at the top of §2.

---

## 1. What we are actually asking

MailPulse is not a high-QPS public API. A few hundred SPA users generate
trivial HTTP traffic. The thing that scales — and the thing that will fall over
first — is the **worker**, because every mail account is polled forever whether
anyone is looking or not.

So the headline question is not "how many requests per second". It is:

> How many mail accounts can one worker keep on their poll interval, and what
> happens to the ones it cannot?

Both worker loops share the same shape, and therefore the same capacity model:

```
batch × average_time_per_item   must stay under   interval
```

| Loop | Interval | Batch | Budget per item | Where it goes |
|------|----------|-------|-----------------|---------------|
| poller | 30s | 20 | 1.5s | a real IMAP dial + fetch |
| dispatcher | 5s | 50 | 0.1s | an outbound HTTP call |
| credential checker | 3600s | 20 | 180s | a real IMAP dial |

Those budgets are tight. `MAIL_IMAP_TIMEOUT` alone is 30s — twenty times the
poller's entire per-account budget.

Two structural facts make this sharper, both confirmed by reading the code
rather than assumed:

**Claimed work is processed strictly sequentially.**
[`PollDue`](../../internal/usecase/pipeline_usecase.go#L83) claims a batch and
then loops `SyncAccount` one at a time; `Tick` does the same for event runs.
There is no concurrency inside a cycle and no per-item deadline, so one slow
mailbox delays every account behind it in the batch. When a cycle overruns its
interval, Go's ticker silently drops ticks — the worker just falls behind, and
nothing says so.

**The credential checker cannot keep up at any real scale.** 20 accounts per
hour is 480 a day. `MAIL_REVERIFY_AFTER` is 6 hours, so the loop can sustain
about 120 accounts. At 10,000 it would take three weeks to get round once. This
is arithmetic, not a measurement — but it is the kind of thing a load test is
supposed to surface, and it is worth confirming before designing around it.

- [ ] Confirm the sequential ceiling by measurement, not arithmetic
- [ ] Decide whether the fix is concurrency within a cycle, a shorter interval,
      more workers, or per-item deadlines — the load test should tell us which

---

## 2. Prerequisite: there is nothing to measure with

There is no Prometheus, no pprof, no expvar, no metrics of any kind. A load test
against a black box can only report "it got slower", never why.

**Corrected 2026-08-27.** This was stated as a hard prerequisite and it is not
one. It is right for observing a *running* worker; it is wrong for a controlled
experiment, where the harness drives `PollDue` itself and is therefore its own
instrument — it times its own calls, and queue depth is one `COUNT(*)`. The
capacity work in §6 was done without any of this. Metrics are still needed to
watch production, which is a different job.

- [ ] `net/http/pprof` on the worker and web binaries, bound to a separate
      admin port and off by default
- [ ] Prometheus metrics and a `/metrics` endpoint

The gauges that answer the questions above:

| Metric | Type | Why |
|--------|------|-----|
| `mailpulse_poll_cycle_seconds` | histogram | is a cycle outrunning its interval |
| `mailpulse_sync_seconds{provider}` | histogram | per-account cost, the budget above |
| `mailpulse_accounts_due` | gauge | **queue depth — the real health signal** |
| `mailpulse_event_runs_pending` | gauge | same, for dispatch |
| `mailpulse_dispatch_seconds{channel}` | histogram | which channel is slow |
| `mailpulse_ticks_skipped_total` | counter | cycles that overran |
| `db_pool_*`, `redis_hit_ratio` | gauge | is the pool or the cache the wall |

Queue depth is the one that matters most. Throughput numbers flatter a system
that is quietly falling behind; a depth gauge that climbs monotonically is the
unambiguous statement that capacity has been exceeded.

---

## 3. Kinds of test

These are different questions and belong in different places. Conflating them is
how a suite becomes slow and ignored.

| Kind | Question | Tool | Lives in | CI |
|------|----------|------|----------|-----|
| Benchmark | what does one operation cost | `testing.B` | beside the code | yes, with a regression gate |
| Load | does the API hold its SLO at expected concurrency | k6 | `test/load/k6/` | nightly |
| Capacity | how many accounts per worker before it falls behind | Go harness | `test/load/` | nightly |
| Stress | what breaks first past capacity, and does it corrupt | Go harness | `test/load/` | manual |
| Soak | leaks, drift, unbounded growth over hours | Go harness | `test/load/` | weekly |
| Contention | do N workers share the queue or collide | Go harness | `test/load/` | manual |

**Benchmarks** are the cheapest and the only ones worth gating CI on. The
matcher runs per email per watcher and is pure — `BenchmarkMatch` over the
operator set, plus the AES-GCM cipher, gives a regression signal for pennies.
Use `benchstat` against the previous run rather than absolute thresholds, which
are meaningless on shared CI hardware.

**Capacity and stress are not HTTP tests**, so k6 is the wrong tool. They drive
`PollDue` and `Tick` directly against a seeded database and measure cycle time
and queue depth. This is the part most load-testing plans get wrong by reaching
for a request generator when the system under test is a queue drainer.

- [x] Benchmarks for matcher and cipher — `internal/usecase/matcher_bench_test.go`
      and `internal/gateway/secret/cipher_bench_test.go`, run by `make bench`.
      Converters are not covered; nothing suggested they mattered
- [ ] `benchstat` gate in CI once a pipeline exists ([03](03-platform-gaps.md))

---

## 4. Abstractions to build

This is most of the actual work, and it is what makes the tests repeatable
rather than a one-off afternoon with `ab`.

### 4a. A load-controllable mail provider

The single most important piece. You cannot load test the pipeline against
GreenMail — it is one container, it is not the thing under test, and its
latency would dominate every measurement.

The [`stub` provider](../../internal/gateway/mail/stub/stub.go) is the seam that
already exists, but it is fixed: it synthesises a handful of messages with no
latency. Make its behaviour configurable:

```go
// internal/gateway/mail/loadstub
type Config struct {
    MessagesPerSync  int           // volume per cycle
    Latency          Distribution  // p50/p95 dial+fetch, not a constant
    ErrorRate        float64       // transient failures
    TimeoutRate      float64       // mailboxes that hang to MAIL_IMAP_TIMEOUT
    BodySize         ByteRange     // MIME parse cost is size-dependent
}
```

Latency must be a **distribution, not a constant**. A constant hides the exact
failure this system has: sequential processing means the tail dominates the
cycle, so p99 mailbox latency sets worker capacity, not the mean.

The registry is keyed by kind and registration happens in
[`Bootstrap`](../../internal/config/app.go), so this is a registration change.
Nothing in the pipeline, the matcher or the routes is touched.

- [x] `loadstub` provider with the knobs above, in
      `internal/gateway/mail/loadstub/`. Latency is lognormal fitted to `-p50`
      and `-p99`, which is what makes the tail experiment possible
- [x] Not selected by env in the end — `cmd/loadtest` registers it into the
      container's own registry after `Bootstrap`, so no production wiring can
      reach it and no env flag has to be trusted. `Container.Providers` was
      exposed for this

### 4b. A null notifier channel

Dispatch load tests must not make real egress calls, or they measure the
internet. There is currently no null channel — the closest is `webhook`, which
really does POST.

- [ ] `null` channel: records the delivery, no egress
- [ ] `slow` variant with configurable latency, to model a throttling Telegram

### 4c. A bulk fixture builder

Creating 10,000 accounts through the HTTP API is itself a load test and takes
long enough to discourage running anything. Fixtures go in through batched
`INSERT`, bypassing usecases deliberately.

```go
// test/load/fixtures
type Shape struct {
    Users            int
    AccountsPerUser  int
    WatchersPerUser  int
    FiltersPerWatcher int
    EventsPerWatcher int
    MatchRate        float64 // fraction of synthetic mail that matches
}
```

Determinism matters: seed the RNG so two runs are comparable. Bypassing the
usecases is a real tradeoff — fixtures can drift from what the API would
actually produce — so the builder needs one integration test asserting a
generated row set is shaped like an API-created one.

- [x] Bulk builder, deterministic, batched inserts — `internal/loadtest/fixtures/`.
      Everything it writes is marked `@loadtest.invalid`, so cleanup is exact
      rather than a truncate that would take the developer's own rows with it
- [ ] A guard test that fixtures match API-created shape

### 4d. One scenario definition, shared

If the k6 script and the worker harness describe their world differently, their
numbers cannot be reasoned about together. One scenario struct, consumed by
both, serialised into the results.

- [ ] `Scenario` = `Shape` + provider `Config` + duration + concurrency
- [ ] Named scenarios checked in: `smoke`, `nominal`, `10k-accounts`, `tail-heavy`

### 4e. A results format

A load test whose output is terminal scrollback cannot show a regression. Emit
JSON per run — scenario, environment, git SHA, and the metrics — into
`test/load/results/`, so runs diff.

- [x] JSON emitter, one file per run, into `test/load/results/`
- [ ] `make load-compare` between two result files

### 4f. A clock seam

Recurrence and the credential re-check are hour-scale behaviours. Testing them
in real time is not viable, and `time.Now()` is currently called directly
throughout the dispatcher.

This is a genuine refactor, not a test-only change, and it should be sized
honestly: an injected `Clock` interface threaded through the dispatcher and the
worker loops. It also unblocks the cron-expansion work in
[03](03-platform-gaps.md), which needs the same seam to be testable at all.

- [ ] `Clock` interface, real implementation in production, fake in tests
- [ ] Threaded through dispatcher and worker loops

---

## 5. Endpoints worth loading

Not all 73. The ones where load reveals something:

- [x] **`GET /api/admin/users`** — `ListUsers` called `countsFor` inside the
      row loop, four count queries each, so `size=100` was 400 queries in one
      request. **Measured 2026-08-27: 96ms p50 against 711µs for
      `/api/matches` — 136x. Fixed 2026-08-28 and re-measured: 7.8ms p50,
      12.6x faster, 1,209 req/s.** Counts are now four grouped queries per
      page, pinned by `TestAdminUserListCounts` including the zero for a user
      who owns nothing and a cross-check against the single-user detail path.
- [ ] **`GET /api/dashboard/summary`, `/api/matches`, `/api/event-runs`** — what
      the SPA polls, paginated with counts, most exposed to a missing index.
- [ ] **`POST /api/users/_login`** — bcrypt is deliberately expensive, so this
      is a CPU wall by design. Worth quantifying how few concurrent logins
      saturate a core, and confirming the new rate limiter caps it before the
      CPU does.
- [ ] **The auth middleware itself** — every authenticated request hits Redis.
      Measure the hit ratio under load, and what a Redis stall does to p99 now
      that `Verify` reads through it.
- [ ] **`_verify` / `_sync`** — synchronous, blocking up to 30s.
      [03](03-platform-gaps.md) already wants these queued; a load test turns
      "should probably" into a number.

---

## 6. Worker and cron experiments

- [x] **Sequential ceiling.** ~226 accounts at a 30s interval with p50 150ms /
      p99 2s, batch 20. `make load-worker ARGS="-accounts=500 -cycles=20"`
- [ ] **Tail sensitivity.** Same mean latency, different p99. Sweep `-p99` with
      `-p50` fixed. The hung-mailbox result above already points at the answer;
      this is the measurement that quantifies it.
- [x] **One hung mailbox.** Devastating, and the best argument in this file.
      At `-timeout-rate=0.05` with a 30s timeout, capacity fell from ~226 to
      **~19** and p99 cycle time hit 1m36s against a 30s interval. One bad
      mailbox in twenty costs 92% of the worker's capacity, because the batch
      is drained sequentially with no per-item deadline.
- [ ] **Multi-worker scaling.** 1, 2, 4, 8 workers on one queue. `SKIP LOCKED`
      should give near-linear throughput; anything else means contention in the
      claim transaction, which also updates `next_poll_at` row by row.
- [ ] **Credential checker arithmetic.** Confirm the ceiling in §1 and size the
      fix.
- [ ] **Recurrence.** With the clock seam, run a repeat chain through thousands
      of occurrences in seconds and confirm `repeat_max`, `stop_on_ack` and
      cancel-after all terminate. Overlaps the dispatcher test gap already open
      in [03](03-platform-gaps.md) — do it once, there.

---

## 7. Kafka — not yet

Kafka currently carries only `user` events (register / login / update), consumed
by a handler that logs them, and `KAFKA_PRODUCER_ENABLED` defaults to `false`.
Nothing in the mail pipeline touches it.

Load testing that today would measure a demo path that is off in production.
**Do not build Kafka load tests until the Kafka decision in
[03](03-platform-gaps.md) is made.** If `email.matched` gets wired between sync
and dispatch, this becomes worth doing properly and the questions are:

- consumer lag under a sync burst, and whether it recovers
- rebalance behaviour when a worker dies mid-batch
- duplicate handling — at-least-once delivery against the
  `(watcher_id, message_id)` dedupe index, which should already absorb it

If Kafka is dropped instead, this section goes away with it.

- [ ] Revisit once the Kafka decision is made — not before

---

## 8. What "pass" means

A load test with no pass criterion is a demo. Each scenario declares thresholds,
and the run fails if they are missed:

- API p95 under the nominal scenario, per endpoint group
- worker sustains its interval at the scenario's account count
- **queue depth flat, not climbing** — the real definition of keeping up, and
  the one assertion that cannot be gamed by a flattering throughput number
- no goroutine or connection growth across a soak
- error rate zero outside the injected `ErrorRate`

---

## 9. Where it lives

```
test/load/
    fixtures/     bulk builders
    scenarios/    named Scenario definitions
    harness/      cycle driver, metrics reader, JSON emitter
    results/      one JSON per run, committed
    k6/           HTTP scripts
```

Behind a `//go:build load` tag, so `go test ./...` never picks it up — the same
split [03](03-platform-gaps.md) already established for integration and feature.

- [x] `make load-smoke` (minutes, CI-able), plus `make load-parser`,
      `load-worker`, `load-endpoints` and `make load` to list them. Nominal and
      soak are flag combinations rather than their own targets so far
- [ ] A compose profile with realistic pool sizes and N workers

---

## 10. Order

1. Metrics and pprof (§2). Everything else is guesswork without them.
2. Benchmarks (§3) — cheap, immediately useful, gate CI.
3. `loadstub` + null channel + fixtures (§4a–c). The minimum to run anything.
4. Worker capacity experiments (§6). The highest-value answers in the plan.
5. Scenario + results format (§4d–e), once there is something worth diffing.
6. Endpoint load (§5).
7. Clock seam (§4f) — pair it with the cron work in [03](03-platform-gaps.md)
   rather than doing it twice.
8. Kafka (§7) — only if it survives.

Steps 1 and 4 are the ones worth doing even if the rest is dropped: they answer
"how many accounts can we take on" — the only capacity question anyone will
actually be asked.

---

## Non-goals

**These numbers will not be production capacity.** The dev stack is one Postgres
container sharing a laptop with the IDE and a browser. The value is *relative* —
regression detection and finding structural ceilings like the sequential loop —
not an absolute figure for a spec sheet. Anything quoted externally needs a
representative environment first.

**Not testing IMAP servers.** `loadstub` deliberately replaces the mailbox. Real
IMAP behaviour is the feature suite's job, against GreenMail, at low volume.
