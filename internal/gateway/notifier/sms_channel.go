package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"mailpulse/internal/model"

	"github.com/gofiber/fiber/v2"
)

type smsConfig struct {
	Phone string `json:"phone"`
}

type smsSecrets struct {
	AccountSID string `json:"account_sid"`
	AuthToken  string `json:"auth_token"`
	FromNumber string `json:"from_number"`
}

// SMSChannel sends through Twilio's REST API. Without credentials it fails
// with a message naming what is missing, rather than pretending to deliver.
type SMSChannel struct {
	Client *http.Client
}

func NewSMSChannel() *SMSChannel {
	return &SMSChannel{Client: &http.Client{Timeout: 20 * time.Second}}
}

func (c *SMSChannel) Type() string               { return "sms" }
func (c *SMSChannel) Label() string              { return "SMS" }
func (c *SMSChannel) Description() string        { return "Text a phone number through Twilio." }
func (c *SMSChannel) RequiresVerification() bool { return true }

func (c *SMSChannel) ConfigSchema() model.Schema {
	return model.Schema{Fields: []model.SchemaField{
		{Name: "phone", Label: "Phone number", Type: "string", Required: true, Placeholder: "+15551234567"},
		{Name: "account_sid", Label: "Twilio account SID", Type: "secret", Secret: true, Required: true},
		{Name: "auth_token", Label: "Twilio auth token", Type: "secret", Secret: true, Required: true},
		{Name: "from_number", Label: "Twilio from number", Type: "secret", Secret: true, Required: true},
	}}
}

func (c *SMSChannel) Validate(config json.RawMessage) error {
	var cfg smsConfig
	if err := decode(config, &cfg); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "sms config is not valid JSON")
	}
	if !strings.HasPrefix(cfg.Phone, "+") || len(cfg.Phone) < 8 {
		return fiber.NewError(fiber.StatusBadRequest, "sms config needs a phone number in E.164 form, for example +15551234567")
	}
	return nil
}

func (c *SMSChannel) Send(ctx context.Context, config, secrets json.RawMessage, message Message) (string, error) {
	var cfg smsConfig
	if err := decode(config, &cfg); err != nil {
		return "", err
	}

	var sec smsSecrets
	if err := decode(secrets, &sec); err != nil {
		return "", err
	}

	var missing []string
	if sec.AccountSID == "" {
		missing = append(missing, "account_sid")
	}
	if sec.AuthToken == "" {
		missing = append(missing, "auth_token")
	}
	if sec.FromNumber == "" {
		missing = append(missing, "from_number")
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("sms: notifier is missing %s", strings.Join(missing, ", "))
	}

	body := message.Title
	if message.Body != "" {
		body += " — " + message.Body
	}
	if len(body) > 320 {
		body = body[:317] + "..."
	}

	form := url.Values{}
	form.Set("To", cfg.Phone)
	form.Set("From", sec.FromNumber)
	form.Set("Body", body)

	endpoint := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", sec.AccountSID)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.SetBasicAuth(sec.AccountSID, sec.AuthToken)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := c.Client.Do(request)
	if err != nil {
		return "", fmt.Errorf("sms: %w", err)
	}
	defer response.Body.Close()

	var payload struct {
		SID     string `json:"sid"`
		Message string `json:"message"`
	}
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
	_ = json.Unmarshal(raw, &payload)

	if response.StatusCode < 200 || response.StatusCode > 299 {
		if payload.Message != "" {
			return "", fmt.Errorf("sms: %s", payload.Message)
		}
		return "", fmt.Errorf("sms: twilio returned %d", response.StatusCode)
	}

	return payload.SID, nil
}
