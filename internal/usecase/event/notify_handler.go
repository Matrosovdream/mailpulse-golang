package event

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"text/template"

	"mailpulse/internal/entity"
	"mailpulse/internal/gateway/notifier"
	"mailpulse/internal/gateway/secret"
	"mailpulse/internal/model"

	"github.com/gofiber/fiber/v2"
)

type notifyConfig struct {
	Template string `json:"template"`
	Title    string `json:"title"`
}

// NotifyHandler is the default event: render one message and fan it out to
// every notifier attached to the event. It is the only handler that touches
// notifiers, which is why the channel registry is injected here and nowhere
// else in the event layer.
type NotifyHandler struct {
	Channels *notifier.Registry
	Cipher   *secret.Cipher
	BaseURL  string
}

func NewNotifyHandler(channels *notifier.Registry, cipher *secret.Cipher, baseURL string) *NotifyHandler {
	return &NotifyHandler{Channels: channels, Cipher: cipher, BaseURL: baseURL}
}

func (h *NotifyHandler) Type() string        { return "notify" }
func (h *NotifyHandler) Label() string       { return "Send a notification" }
func (h *NotifyHandler) UsesNotifiers() bool { return true }

func (h *NotifyHandler) Description() string {
	return "Send the match to one or more of your connected notifiers."
}

func (h *NotifyHandler) ConfigSchema() model.Schema {
	return model.Schema{Fields: []model.SchemaField{
		{
			Name: "title", Label: "Title", Type: "string",
			Placeholder: "Watcher {{.WatcherName}} matched",
			Help:        "Go template. Available: .Subject .FromAddress .FromName .WatcherName .Occurrence",
		},
		{
			Name: "template", Label: "Message", Type: "text",
			Placeholder: "{{.Subject}} from {{.FromAddress}}",
			Help:        "Go template over the matched email. Leave empty for the default summary.",
		},
	}}
}

func (h *NotifyHandler) Validate(config json.RawMessage) error {
	var cfg notifyConfig
	if len(config) > 0 {
		if err := json.Unmarshal(config, &cfg); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "notify config is not valid JSON")
		}
	}

	// fail template syntax at write time rather than at 3am inside the worker
	for name, text := range map[string]string{"title": cfg.Title, "template": cfg.Template} {
		if text == "" {
			continue
		}
		if _, err := template.New(name).Parse(text); err != nil {
			return fiber.NewError(fiber.StatusBadRequest,
				fmt.Sprintf("notify %s is not a valid template: %v", name, err))
		}
	}

	return nil
}

type templateData struct {
	Subject     string
	FromAddress string
	FromName    string
	Snippet     string
	WatcherName string
	Occurrence  int
	ReceivedAt  int64
}

func (h *NotifyHandler) Execute(ctx context.Context, in Input) (Output, error) {
	var cfg notifyConfig
	if len(in.Config) > 0 {
		_ = json.Unmarshal(in.Config, &cfg)
	}

	data := templateData{
		Subject:     derefString(in.Email.Subject),
		FromAddress: derefString(in.Email.FromAddress),
		FromName:    derefString(in.Email.FromName),
		Snippet:     derefString(in.Email.Snippet),
		WatcherName: in.Watcher.Name,
		Occurrence:  in.Occurrence,
		ReceivedAt:  in.Email.ReceivedAt,
	}

	title := render(cfg.Title, data, fmt.Sprintf("%s matched", in.Watcher.Name))
	body := render(cfg.Template, data, fmt.Sprintf("%s\nfrom %s", data.Subject, data.FromAddress))

	if in.Occurrence > 1 {
		title = fmt.Sprintf("%s (reminder %d)", title, in.Occurrence)
	}

	message := notifier.Message{
		Title: title,
		Body:  body,
		URL:   fmt.Sprintf("%s/matches/%s", h.BaseURL, in.Email.ID),
		Meta: map[string]string{
			"watcher_id": in.Watcher.ID,
			"match_id":   in.Email.ID,
		},
	}

	if len(in.Notifiers) == 0 {
		return Output{}, fiber.NewError(fiber.StatusUnprocessableEntity,
			"notify event has no notifiers attached")
	}

	output := Output{Deliveries: make([]Delivery, 0, len(in.Notifiers))}
	var failures int

	for i := range in.Notifiers {
		target := in.Notifiers[i]
		delivery := Delivery{
			NotifierID:      &target.ID,
			ChannelType:     target.Type,
			RenderedMessage: title + "\n" + body,
		}

		channel, err := h.Channels.Get(target.Type)
		if err != nil {
			delivery.Status = entity.DeliveryStatusFailed
			delivery.Error = err.Error()
			failures++
			output.Deliveries = append(output.Deliveries, delivery)
			continue
		}

		if target.Status != entity.NotifierStatusVerified {
			delivery.Status = entity.DeliveryStatusFailed
			delivery.Error = fmt.Sprintf("notifier %q is %s, not verified", target.Name, target.Status)
			failures++
			output.Deliveries = append(output.Deliveries, delivery)
			continue
		}

		secrets, err := h.decryptSecrets(&target)
		if err != nil {
			delivery.Status = entity.DeliveryStatusFailed
			delivery.Error = err.Error()
			failures++
			output.Deliveries = append(output.Deliveries, delivery)
			continue
		}

		providerID, err := channel.Send(ctx, json.RawMessage(target.Config), secrets, message)
		if err != nil {
			delivery.Status = entity.DeliveryStatusFailed
			delivery.Error = err.Error()
			failures++
		} else {
			delivery.Status = entity.DeliveryStatusSent
			delivery.ProviderMessageID = providerID
		}

		output.Deliveries = append(output.Deliveries, delivery)
	}

	output.Result, _ = json.Marshal(map[string]any{
		"sent":   len(output.Deliveries) - failures,
		"failed": failures,
	})

	// every destination failing is a failed run, worth retrying; a partial
	// failure is recorded per delivery but does not re-send to the ones that
	// already worked
	if failures == len(output.Deliveries) {
		return output, fmt.Errorf("all %d deliveries failed", failures)
	}

	return output, nil
}

func (h *NotifyHandler) decryptSecrets(target *entity.Notifier) (json.RawMessage, error) {
	if target.Secrets == nil || *target.Secrets == "" {
		return nil, nil
	}

	plaintext, err := h.Cipher.Decrypt(*target.Secrets)
	if err != nil {
		return nil, fmt.Errorf("could not decrypt notifier secrets: %w", err)
	}

	return json.RawMessage(plaintext), nil
}

func render(text string, data templateData, fallback string) string {
	if text == "" {
		return fallback
	}

	tmpl, err := template.New("t").Parse(text)
	if err != nil {
		return fallback
	}

	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return fallback
	}

	return out.String()
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
