package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// silentFor backdates a loop's last tick, which is how staleness is tested
// without a clock seam or a sleeping test.
func silentFor(t *testing.T, h *loopHealth, name string, d time.Duration) {
	t.Helper()

	h.mu.Lock()
	defer h.mu.Unlock()

	state, ok := h.loops[name]
	require.True(t, ok, "loop %q was never registered", name)
	state.lastTick = time.Now().Add(-d)
}

func TestFreshlyRegisteredLoopIsNotStalled(t *testing.T) {
	// a worker that has only just booted has not reached its first tick yet,
	// and must not be reported unhealthy for it
	loops := newLoopHealth()
	loops.register("poller", 30*time.Second)

	report, stalled := loops.report()

	assert.False(t, stalled)
	assert.False(t, report["poller"].Stalled)
	assert.Equal(t, 0, report["poller"].SilentSeconds)
}

func TestBudgetIsThreeIntervalsWithAFloor(t *testing.T) {
	loops := newLoopHealth()
	loops.register("poller", 30*time.Second)    // 90s
	loops.register("dispatcher", 5*time.Second) // 15s, under the floor
	loops.register("credentials", time.Hour)    // 3h

	report, _ := loops.report()

	assert.Equal(t, 90, report["poller"].BudgetSeconds)
	assert.Equal(t, 60, report["dispatcher"].BudgetSeconds,
		"a fast loop gets the one minute floor, not three intervals")
	assert.Equal(t, 3*3600, report["credentials"].BudgetSeconds)
}

func TestALateButTickingLoopIsHealthy(t *testing.T) {
	// one slow cycle is normal under load; only sustained silence is a stall
	loops := newLoopHealth()
	loops.register("poller", 30*time.Second)

	silentFor(t, loops, "poller", 80*time.Second)

	report, stalled := loops.report()

	assert.False(t, stalled)
	assert.False(t, report["poller"].Stalled)
	assert.Equal(t, 80, report["poller"].SilentSeconds)
}

func TestASilentLoopStallsAndFailsTheWholeReport(t *testing.T) {
	loops := newLoopHealth()
	loops.register("poller", 30*time.Second)
	loops.register("dispatcher", 5*time.Second)

	silentFor(t, loops, "poller", 5*time.Minute)

	report, stalled := loops.report()

	assert.True(t, stalled, "one stalled loop makes the worker unhealthy")
	assert.True(t, report["poller"].Stalled)
	assert.False(t, report["dispatcher"].Stalled, "the healthy loop is still named as healthy")
}

func TestTickClearsAStall(t *testing.T) {
	loops := newLoopHealth()
	loops.register("poller", 30*time.Second)

	silentFor(t, loops, "poller", 5*time.Minute)
	_, stalled := loops.report()
	require.True(t, stalled)

	loops.tick("poller")

	report, stalled := loops.report()
	assert.False(t, stalled)
	assert.Equal(t, 0, report["poller"].SilentSeconds)
}

func TestTickingAnUnregisteredLoopIsANoOp(t *testing.T) {
	// the loops register themselves, so a tick can in principle arrive for a
	// name the tracker has not seen; it must not panic or invent an entry
	loops := newLoopHealth()

	assert.NotPanics(t, func() { loops.tick("nobody") })

	report, stalled := loops.report()
	assert.Empty(t, report)
	assert.False(t, stalled)
}
