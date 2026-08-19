package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"net/smtp"
	"strings"

	"mailpulse/internal/model"

	"github.com/gofiber/fiber/v2"
)

type emailConfig struct {
	To string `json:"to"`
}

type emailSecrets struct {
	Host     string `json:"smtp_host"`
	Port     int    `json:"smtp_port"`
	Username string `json:"smtp_username"`
	Password string `json:"smtp_password"`
	From     string `json:"smtp_from"`
}

// EmailChannel sends over SMTP. Notifying by email about email is circular by
// nature, so this is most useful pointed at a different mailbox than the one
// being watched.
type EmailChannel struct{}

func NewEmailChannel() *EmailChannel { return &EmailChannel{} }

func (c *EmailChannel) Type() string               { return "email" }
func (c *EmailChannel) Label() string              { return "Email" }
func (c *EmailChannel) Description() string        { return "Send an email through your own SMTP server." }
func (c *EmailChannel) RequiresVerification() bool { return true }

func (c *EmailChannel) ConfigSchema() model.Schema {
	return model.Schema{Fields: []model.SchemaField{
		{Name: "to", Label: "Recipient", Type: "string", Required: true, Placeholder: "alerts@example.com"},
		{Name: "smtp_host", Label: "SMTP host", Type: "secret", Secret: true, Required: true},
		{Name: "smtp_port", Label: "SMTP port", Type: "int", Secret: true},
		{Name: "smtp_username", Label: "SMTP username", Type: "secret", Secret: true},
		{Name: "smtp_password", Label: "SMTP password", Type: "secret", Secret: true},
		{Name: "smtp_from", Label: "From address", Type: "secret", Secret: true},
	}}
}

func (c *EmailChannel) Validate(config json.RawMessage) error {
	var cfg emailConfig
	if err := decode(config, &cfg); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "email config is not valid JSON")
	}
	if !strings.Contains(cfg.To, "@") {
		return fiber.NewError(fiber.StatusBadRequest, "email config needs a recipient address")
	}
	return nil
}

func (c *EmailChannel) Send(ctx context.Context, config, secrets json.RawMessage, message Message) (string, error) {
	var cfg emailConfig
	if err := decode(config, &cfg); err != nil {
		return "", err
	}

	var sec emailSecrets
	if err := decode(secrets, &sec); err != nil {
		return "", err
	}

	if sec.Host == "" {
		return "", fmt.Errorf("email: notifier is missing smtp_host")
	}
	if sec.Port == 0 {
		sec.Port = 587
	}
	if sec.From == "" {
		sec.From = sec.Username
	}

	body := message.Body
	if message.URL != "" {
		body += "\r\n\r\n" + message.URL
	}

	payload := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s\r\n",
		sec.From, cfg.To, message.Title, body))

	addr := fmt.Sprintf("%s:%d", sec.Host, sec.Port)

	var auth smtp.Auth
	if sec.Username != "" {
		auth = smtp.PlainAuth("", sec.Username, sec.Password, sec.Host)
	}

	if err := smtp.SendMail(addr, auth, sec.From, []string{cfg.To}, payload); err != nil {
		return "", fmt.Errorf("email: %w", err)
	}

	return "", nil
}
