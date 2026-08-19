package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"mailpulse/internal/model"

	"github.com/gofiber/fiber/v2"
)

type telegramConfig struct {
	ChatID string `json:"chat_id"`
}

type telegramSecrets struct {
	BotToken string `json:"bot_token"`
}

// TelegramChannel talks to the Bot API directly; there is no SDK worth the
// dependency for one endpoint.
type TelegramChannel struct {
	Client       *http.Client
	DefaultToken string
}

func NewTelegramChannel(defaultToken string) *TelegramChannel {
	return &TelegramChannel{
		Client:       &http.Client{Timeout: 15 * time.Second},
		DefaultToken: defaultToken,
	}
}

func (c *TelegramChannel) Type() string  { return "telegram" }
func (c *TelegramChannel) Label() string { return "Telegram" }

func (c *TelegramChannel) Description() string {
	return "Send a message to a Telegram chat through your bot."
}

func (c *TelegramChannel) RequiresVerification() bool { return true }

func (c *TelegramChannel) ConfigSchema() model.Schema {
	return model.Schema{Fields: []model.SchemaField{
		{
			Name: "chat_id", Label: "Chat ID", Type: "string", Required: true,
			Placeholder: "123456789",
			Help:        "Message your bot, then send the verification code back to it to bind this chat.",
		},
		{
			Name: "bot_token", Label: "Bot token", Type: "secret", Secret: true,
			Help: "Leave empty to use the server's bot.",
		},
	}}
}

func (c *TelegramChannel) Validate(config json.RawMessage) error {
	var cfg telegramConfig
	if err := decode(config, &cfg); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "telegram config is not valid JSON")
	}
	if strings.TrimSpace(cfg.ChatID) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "telegram config needs a chat_id")
	}
	return nil
}

func (c *TelegramChannel) Send(ctx context.Context, config, secrets json.RawMessage, message Message) (string, error) {
	var cfg telegramConfig
	if err := decode(config, &cfg); err != nil {
		return "", err
	}

	var sec telegramSecrets
	if err := decode(secrets, &sec); err != nil {
		return "", err
	}

	token := sec.BotToken
	if token == "" {
		token = c.DefaultToken
	}
	if token == "" {
		return "", fmt.Errorf("telegram: no bot token configured for this notifier or the server")
	}

	text := message.Title
	if message.Body != "" {
		text += "\n\n" + message.Body
	}
	if message.URL != "" {
		text += "\n\n" + message.URL
	}

	payload, err := json.Marshal(map[string]any{
		"chat_id":                  cfg.ChatID,
		"text":                     text,
		"disable_web_page_preview": true,
	})
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := c.Client.Do(request)
	if err != nil {
		return "", fmt.Errorf("telegram: %w", err)
	}
	defer response.Body.Close()

	var body struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      struct {
			MessageID int64 `json:"message_id"`
		} `json:"result"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("telegram: unreadable response (status %d)", response.StatusCode)
	}

	if !body.OK {
		return "", fmt.Errorf("telegram: %s", body.Description)
	}

	return fmt.Sprint(body.Result.MessageID), nil
}
