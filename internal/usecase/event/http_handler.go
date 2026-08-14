package event

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"mailpulse/internal/model"

	"github.com/gofiber/fiber/v2"
)

type httpConfig struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// HTTPHandler covers both "webhook" and "http_request": the same transport,
// registered twice with different labels, because users think of posting a
// match and calling an API as different actions even though they are not.
type HTTPHandler struct {
	Client      *http.Client
	handlerType string
	label       string
	description string
}

func NewWebhookHandler() *HTTPHandler {
	return &HTTPHandler{
		Client:      &http.Client{Timeout: 20 * time.Second},
		handlerType: "webhook",
		label:       "Call a webhook",
		description: "POST the matched email to a URL as JSON.",
	}
}

func NewHTTPRequestHandler() *HTTPHandler {
	return &HTTPHandler{
		Client:      &http.Client{Timeout: 20 * time.Second},
		handlerType: "http_request",
		label:       "Make an HTTP request",
		description: "Call any API with a templated method, headers and body.",
	}
}

func (h *HTTPHandler) Type() string        { return h.handlerType }
func (h *HTTPHandler) Label() string       { return h.label }
func (h *HTTPHandler) Description() string { return h.description }
func (h *HTTPHandler) UsesNotifiers() bool { return false }

func (h *HTTPHandler) ConfigSchema() model.Schema {
	fields := []model.SchemaField{
		{Name: "url", Label: "URL", Type: "string", Required: true, Placeholder: "https://example.com/hook"},
		{Name: "method", Label: "Method", Type: "enum", Options: []string{"POST", "PUT", "PATCH", "GET"}},
	}

	if h.handlerType == "http_request" {
		fields = append(fields,
			model.SchemaField{Name: "headers", Label: "Headers", Type: "text",
				Help: `JSON object, for example {"Authorization": "Bearer ..."}`},
			model.SchemaField{Name: "body", Label: "Body", Type: "text",
				Help: "Go template. Available: .Subject .FromAddress .FromName .WatcherName .Occurrence"},
		)
	}

	return model.Schema{Fields: fields}
}

func (h *HTTPHandler) Validate(config json.RawMessage) error {
	var cfg httpConfig
	if err := json.Unmarshal(nonEmpty(config), &cfg); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, h.handlerType+" config is not valid JSON")
	}

	if !strings.HasPrefix(cfg.URL, "http://") && !strings.HasPrefix(cfg.URL, "https://") {
		return fiber.NewError(fiber.StatusBadRequest, h.handlerType+" config needs an http or https url")
	}

	switch cfg.Method {
	case "", http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodGet:
	default:
		return fiber.NewError(fiber.StatusBadRequest, "method must be one of POST, PUT, PATCH, GET")
	}

	return nil
}

func (h *HTTPHandler) Execute(ctx context.Context, in Input) (Output, error) {
	var cfg httpConfig
	if err := json.Unmarshal(nonEmpty(in.Config), &cfg); err != nil {
		return Output{}, err
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

	var payload []byte
	if cfg.Body != "" {
		payload = []byte(render(cfg.Body, data, ""))
	} else {
		payload, _ = json.Marshal(map[string]any{
			"watcher_id":   in.Watcher.ID,
			"watcher_name": in.Watcher.Name,
			"match_id":     in.Email.ID,
			"occurrence":   in.Occurrence,
			"subject":      data.Subject,
			"from_address": data.FromAddress,
			"from_name":    data.FromName,
			"snippet":      data.Snippet,
			"received_at":  in.Email.ReceivedAt,
		})
	}

	method := cfg.Method
	if method == "" {
		method = http.MethodPost
	}

	var reader io.Reader
	if method != http.MethodGet {
		reader = bytes.NewReader(payload)
	}

	request, err := http.NewRequestWithContext(ctx, method, cfg.URL, reader)
	if err != nil {
		return Output{}, err
	}

	request.Header.Set("Content-Type", "application/json")
	for key, value := range cfg.Headers {
		request.Header.Set(key, value)
	}

	response, err := h.Client.Do(request)
	if err != nil {
		return Output{}, fmt.Errorf("%s: %w", h.handlerType, err)
	}
	defer response.Body.Close()

	// keep a bounded slice of the response so the run log can show what came
	// back without storing a megabyte of HTML
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))

	result, _ := json.Marshal(map[string]any{
		"status_code": response.StatusCode,
		"response":    string(body),
	})

	if response.StatusCode < 200 || response.StatusCode > 299 {
		return Output{Result: result}, fmt.Errorf("%s: endpoint returned %d", h.handlerType, response.StatusCode)
	}

	return Output{Result: result}, nil
}

func nonEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return raw
}
