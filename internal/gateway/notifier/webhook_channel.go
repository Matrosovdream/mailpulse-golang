package notifier

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"mailpulse/internal/model"

	"github.com/gofiber/fiber/v2"
)

type webhookConfig struct {
	URL    string `json:"url"`
	Method string `json:"method"`
}

type webhookSecrets struct {
	SigningSecret string `json:"signing_secret"`
}

// WebhookChannel posts the notification as JSON. Slack and Discord incoming
// webhooks are the same shape with a different payload key, so they reuse it
// through payloadKey rather than duplicating the transport.
type WebhookChannel struct {
	Client      *http.Client
	channelType string
	label       string
	description string
	payloadKey  string
}

func NewWebhookChannel() *WebhookChannel {
	return &WebhookChannel{
		Client:      &http.Client{Timeout: 15 * time.Second},
		channelType: "webhook",
		label:       "Webhook",
		description: "POST the match to a URL of your own.",
	}
}

func NewSlackChannel() *WebhookChannel {
	return &WebhookChannel{
		Client:      &http.Client{Timeout: 15 * time.Second},
		channelType: "slack",
		label:       "Slack",
		description: "Post to a Slack channel through an incoming webhook.",
		payloadKey:  "text",
	}
}

func NewDiscordChannel() *WebhookChannel {
	return &WebhookChannel{
		Client:      &http.Client{Timeout: 15 * time.Second},
		channelType: "discord",
		label:       "Discord",
		description: "Post to a Discord channel through an incoming webhook.",
		payloadKey:  "content",
	}
}

func (c *WebhookChannel) Type() string               { return c.channelType }
func (c *WebhookChannel) Label() string              { return c.label }
func (c *WebhookChannel) Description() string        { return c.description }
func (c *WebhookChannel) RequiresVerification() bool { return false }

func (c *WebhookChannel) ConfigSchema() model.Schema {
	fields := []model.SchemaField{
		{Name: "url", Label: "URL", Type: "string", Required: true, Placeholder: "https://example.com/hook"},
	}

	if c.payloadKey == "" {
		fields = append(fields,
			model.SchemaField{Name: "method", Label: "Method", Type: "enum", Options: []string{"POST", "PUT"}},
			model.SchemaField{
				Name: "signing_secret", Label: "Signing secret", Type: "secret", Secret: true,
				Help: "When set, requests carry an X-Mailpulse-Signature HMAC-SHA256 of the body.",
			})
	}

	return model.Schema{Fields: fields}
}

func (c *WebhookChannel) Validate(config json.RawMessage) error {
	var cfg webhookConfig
	if err := decode(config, &cfg); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, c.channelType+" config is not valid JSON")
	}

	if !strings.HasPrefix(cfg.URL, "http://") && !strings.HasPrefix(cfg.URL, "https://") {
		return fiber.NewError(fiber.StatusBadRequest, c.channelType+" config needs an http or https url")
	}

	if cfg.Method != "" && cfg.Method != http.MethodPost && cfg.Method != http.MethodPut {
		return fiber.NewError(fiber.StatusBadRequest, "webhook method must be POST or PUT")
	}

	return nil
}

func (c *WebhookChannel) Send(ctx context.Context, config, secrets json.RawMessage, message Message) (string, error) {
	var cfg webhookConfig
	if err := decode(config, &cfg); err != nil {
		return "", err
	}

	var sec webhookSecrets
	if err := decode(secrets, &sec); err != nil {
		return "", err
	}

	var payload []byte
	var err error

	if c.payloadKey != "" {
		text := message.Title
		if message.Body != "" {
			text += "\n" + message.Body
		}
		if message.URL != "" {
			text += "\n" + message.URL
		}
		payload, err = json.Marshal(map[string]string{c.payloadKey: text})
	} else {
		payload, err = json.Marshal(map[string]any{
			"title": message.Title,
			"body":  message.Body,
			"url":   message.URL,
			"meta":  message.Meta,
		})
	}
	if err != nil {
		return "", err
	}

	method := cfg.Method
	if method == "" {
		method = http.MethodPost
	}

	request, err := http.NewRequestWithContext(ctx, method, cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")

	if sec.SigningSecret != "" {
		mac := hmac.New(sha256.New, []byte(sec.SigningSecret))
		mac.Write(payload)
		request.Header.Set("X-Mailpulse-Signature", hex.EncodeToString(mac.Sum(nil)))
	}

	response, err := c.Client.Do(request)
	if err != nil {
		return "", fmt.Errorf("%s: %w", c.channelType, err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))

	if response.StatusCode < 200 || response.StatusCode > 299 {
		return "", fmt.Errorf("%s: endpoint returned %d", c.channelType, response.StatusCode)
	}

	return response.Header.Get("X-Request-Id"), nil
}
