package event

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"mailpulse/internal/entity"
	"mailpulse/internal/model"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

// Input is everything a handler gets for one occurrence. It is a snapshot:
// Config comes from event_runs.config_snapshot, not from the live watcher, so
// editing a watcher never rewrites what an in-flight run was told to do.
type Input struct {
	Occurrence int
	Watcher    *entity.Watcher
	Event      *entity.WatcherEvent
	Email      *entity.MatchedEmail
	Config     json.RawMessage
	Notifiers  []entity.Notifier
}

// Delivery is one attempt against one destination, written to
// notification_deliveries by the dispatcher.
type Delivery struct {
	NotifierID        *string
	ChannelType       string
	Status            string
	RenderedMessage   string
	ProviderMessageID string
	Error             string
}

type Output struct {
	Result     json.RawMessage
	Deliveries []Delivery
}

// Handler is one thing that can happen when a watcher matches.
//
// Returning an error marks the run failed and schedules a retry, so handlers
// stay stateless and idempotent where they can.
type Handler interface {
	Type() string
	Label() string
	Description() string

	// UsesNotifiers tells the SPA whether to show the notifier picker.
	UsesNotifiers() bool

	// ConfigSchema drives the dynamic form behind GET /api/event-types.
	ConfigSchema() model.Schema

	// Validate runs when the event is written, not when it fires.
	Validate(config json.RawMessage) error

	Execute(ctx context.Context, in Input) (Output, error)
}

type Registry struct {
	handlers map[string]Handler
	Log      *logrus.Logger
}

func NewRegistry(log *logrus.Logger) *Registry {
	return &Registry{handlers: map[string]Handler{}, Log: log}
}

func (r *Registry) Register(handlers ...Handler) {
	for _, handler := range handlers {
		r.handlers[handler.Type()] = handler
	}
}

func (r *Registry) Get(handlerType string) (Handler, error) {
	handler, ok := r.handlers[handlerType]
	if !ok {
		return nil, fiber.NewError(fiber.StatusBadRequest,
			fmt.Sprintf("unknown event type %q", handlerType))
	}
	return handler, nil
}

// Types serves GET /api/event-types.
func (r *Registry) Types() []model.EventTypeResponse {
	types := make([]model.EventTypeResponse, 0, len(r.handlers))
	for _, handler := range r.handlers {
		types = append(types, model.EventTypeResponse{
			Type:          handler.Type(),
			Label:         handler.Label(),
			Description:   handler.Description(),
			UsesNotifiers: handler.UsesNotifiers(),
			ConfigSchema:  handler.ConfigSchema(),
		})
	}

	sort.Slice(types, func(i, j int) bool { return types[i].Type < types[j].Type })
	return types
}
