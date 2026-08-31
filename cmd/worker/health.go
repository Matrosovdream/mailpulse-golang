package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"mailpulse/internal/config"
	"mailpulse/internal/model"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// loopHealth records when each worker loop last came around.
//
// A process check cannot answer the question this exists to answer. The binary
// stays alive while a poll blocks forever on a mailbox that never replies —
// and the poller has no per-item deadline yet, so that is not hypothetical. A
// stalled loop is the failure that actually happens, so the probe watches for
// silence rather than for a dead process.
type loopHealth struct {
	mu    sync.Mutex
	loops map[string]*loopState
}

type loopState struct {
	// budget is how long this loop may stay quiet before it counts as
	// stalled, derived from its own interval: a credential checker that runs
	// hourly is not late at ten minutes.
	budget   time.Duration
	lastTick time.Time
}

// loopReport is one loop's answer. Durations are seconds rather than Go
// duration strings, because the reader is a probe or a dashboard.
type loopReport struct {
	SilentSeconds int  `json:"silent_seconds"`
	BudgetSeconds int  `json:"budget_seconds"`
	Stalled       bool `json:"stalled"`
}

// workerHealth is the whole answer: the same dependency check the web binary
// serves, plus the loops, which only exist in this process.
type workerHealth struct {
	model.HealthResponse
	Loops map[string]loopReport `json:"loops"`
}

func newLoopHealth() *loopHealth {
	return &loopHealth{loops: map[string]*loopState{}}
}

// register declares a loop and its cadence.
//
// It is called by the loop itself rather than by main, so an interval is read
// from configuration in exactly one place. A loop that never registers is not
// a hole in the report: a goroutine that fails to start has panicked, and an
// unrecovered panic takes the process down, where restart policy handles it.
//
// lastTick starts at registration, so a worker that has only just booted is
// not reported stalled for not yet having reached its first tick.
func (h *loopHealth) register(name string, interval time.Duration) {
	// three cycles of slack: one late cycle is normal under load, three in a
	// row is a loop that is not coming back. The floor covers the fast loops,
	// where three intervals is only fifteen seconds.
	budget := 3 * interval
	if budget < time.Minute {
		budget = time.Minute
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.loops[name] = &loopState{budget: budget, lastTick: time.Now()}
}

// tick records that the loop came back around, whether or not the work it did
// succeeded. A failing poll still proves the loop is turning, and the failure
// itself is logged where it happens.
func (h *loopHealth) tick(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if state, ok := h.loops[name]; ok {
		state.lastTick = time.Now()
	}
}

// report returns the per-loop view and whether any loop has overrun its budget.
func (h *loopHealth) report() (map[string]loopReport, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	report := make(map[string]loopReport, len(h.loops))
	stalled := false

	for name, state := range h.loops {
		silent := now.Sub(state.lastTick)
		overrun := silent > state.budget
		if overrun {
			stalled = true
		}

		report[name] = loopReport{
			SilentSeconds: int(silent.Seconds()),
			BudgetSeconds: int(state.budget.Seconds()),
			Stalled:       overrun,
		}
	}

	return report, stalled
}

// serveHealth runs the worker's health listener until ctx is cancelled.
//
// Deliberately a fresh fiber app with one route rather than the one Bootstrap
// was handed: Bootstrap registers the whole route table on whatever app it is
// given, so listening on that one would serve the entire API from the worker.
//
// The response is plain JSON rather than the API's envelope. This is an ops
// endpoint on its own port, not part of the documented API, and it is not in
// api/openapi.yaml.
func serveHealth(ctx context.Context, log *logrus.Logger, viperConfig *viper.Viper,
	container *config.Container, loops *loopHealth) {

	port := viperConfig.GetInt("worker.health_port")

	app := fiber.New(fiber.Config{DisableStartupMessage: true})

	app.Get("/health", func(c *fiber.Ctx) error {
		dependencies := container.Catalog.Health(c.UserContext())
		report, stalled := loops.report()

		status := fiber.StatusOK
		if dependencies.Database != "ok" || dependencies.Redis != "ok" || stalled {
			status = fiber.StatusServiceUnavailable
		}

		return c.Status(status).JSON(workerHealth{
			HealthResponse: *dependencies,
			Loops:          report,
		})
	})

	go func() {
		<-ctx.Done()
		_ = app.ShutdownWithTimeout(5 * time.Second)
	}()

	log.Infof("Worker health listening on :%d/health", port)

	if err := app.Listen(fmt.Sprintf(":%d", port)); err != nil {
		log.WithError(err).Warn("Worker health listener stopped")
	}
}
