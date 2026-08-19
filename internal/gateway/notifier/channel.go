package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"mailpulse/internal/model"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

// Message is what a channel renders and sends. Channels format it for their
// medium; nothing above this layer knows about Telegram markdown or SMS length.
type Message struct {
	Title string
	Body  string
	URL   string
	Meta  map[string]string
}

// Channel is one delivery medium. Adding Discord means writing one of these
// and registering it in Bootstrap — no changes to events, routes or the SPA,
// because the config form is rendered from ConfigSchema.
type Channel interface {
	Type() string
	Label() string
	Description() string

	// RequiresVerification gates whether a notifier must prove ownership of
	// the destination before anything is sent to it.
	RequiresVerification() bool

	ConfigSchema() model.Schema

	// Validate runs on write so a broken config fails at the API rather than
	// at 3am inside the worker.
	Validate(config json.RawMessage) error

	// Send returns the provider's message id for the delivery row.
	Send(ctx context.Context, config, secrets json.RawMessage, message Message) (string, error)
}

type Registry struct {
	channels map[string]Channel
	Log      *logrus.Logger
}

func NewRegistry(log *logrus.Logger) *Registry {
	return &Registry{channels: map[string]Channel{}, Log: log}
}

func (r *Registry) Register(channels ...Channel) {
	for _, channel := range channels {
		r.channels[channel.Type()] = channel
	}
}

func (r *Registry) Get(channelType string) (Channel, error) {
	channel, ok := r.channels[channelType]
	if !ok {
		return nil, fiber.NewError(fiber.StatusBadRequest,
			fmt.Sprintf("unknown notifier type %q", channelType))
	}
	return channel, nil
}

func (r *Registry) Has(channelType string) bool {
	_, ok := r.channels[channelType]
	return ok
}

// Types serves GET /api/notifier-types.
func (r *Registry) Types() []model.NotifierTypeResponse {
	types := make([]model.NotifierTypeResponse, 0, len(r.channels))
	for _, channel := range r.channels {
		types = append(types, model.NotifierTypeResponse{
			Type:                 channel.Type(),
			Label:                channel.Label(),
			Description:          channel.Description(),
			RequiresVerification: channel.RequiresVerification(),
			ConfigSchema:         channel.ConfigSchema(),
		})
	}

	sort.Slice(types, func(i, j int) bool { return types[i].Type < types[j].Type })
	return types
}

// decode is shared by the channels to read their own config and secrets.
func decode(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, target)
}
