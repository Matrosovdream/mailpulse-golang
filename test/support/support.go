//go:build integration || feature

// Package support is the shared harness for the tests that need real
// infrastructure: a Postgres database, Redis, and for some of them a mail
// server. Unit tests never import it — they live beside the code they cover
// and need nothing running.
package support

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"mailpulse/internal/config"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

// Harness is the wired application under test. It is built once per package
// run: bootstrapping is expensive and the state that matters is reset per test
// by Reset instead.
type Harness struct {
	App       *fiber.App
	DB        *gorm.DB
	Redis     *redis.Client
	Config    *viper.Viper
	Log       *logrus.Logger
	Validate  *validator.Validate
	Container *config.Container
}

var (
	shared *Harness
	once   sync.Once
)

// New returns the shared harness, building it on first use.
func New(t *testing.T) *Harness {
	t.Helper()

	once.Do(func() {
		viperConfig := config.NewViper()

		log := config.NewLogger(viperConfig)
		// the application logs at debug in development, which would bury the
		// test output; failures are reported by the assertions instead
		log.SetLevel(logrus.WarnLevel)

		app := config.NewFiber(viperConfig)
		db := config.NewDatabase(viperConfig, log)
		redisClient := config.NewRedis(viperConfig, log)
		validate := config.NewValidator(viperConfig)

		container := config.Bootstrap(&config.BootstrapConfig{
			DB:       db,
			App:      app,
			Log:      log,
			Validate: validate,
			Config:   viperConfig,
			Redis:    redisClient,
			Producer: nil, // kafka is not part of the request path
		})

		shared = &Harness{
			App: app, DB: db, Redis: redisClient, Config: viperConfig,
			Log: log, Validate: validate, Container: container,
		}
	})

	return shared
}

// Reset empties everything a test could have written.
//
// Truncating users cascades through every table that hangs off a user, which
// is all of them except the two reference tables seeded by migrations — those
// are left alone because tests rely on them existing.
func (h *Harness) Reset(t *testing.T) {
	t.Helper()

	if err := h.DB.Exec("TRUNCATE users CASCADE").Error; err != nil {
		t.Fatalf("could not reset the database: %v", err)
	}

	// cached auth would otherwise let a token from a previous test keep working,
	// and the rate limit counters would carry across tests: every request in the
	// suite arrives from the same address, so one test's logins would otherwise
	// spend the next test's allowance
	ctx := context.Background()
	keys, err := h.Redis.Keys(ctx, "user:token:*").Result()
	if err == nil && len(keys) > 0 {
		h.Redis.Del(ctx, keys...)
	}

	limits, err := h.Redis.Keys(ctx, "ratelimit:*").Result()
	if err == nil && len(limits) > 0 {
		h.Redis.Del(ctx, limits...)
	}
}

// ---------------------------------------------------------------- HTTP

// Response is a decoded API response, which is always the same envelope.
type Response struct {
	Status int
	Body   []byte
}

// Decode unpacks the "data" member into target.
func (r Response) Decode(t *testing.T, target any) {
	t.Helper()

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors string          `json:"errors"`
	}
	if err := json.Unmarshal(r.Body, &envelope); err != nil {
		t.Fatalf("response is not the standard envelope: %v\nbody: %s", err, r.Body)
	}
	if len(envelope.Data) == 0 {
		t.Fatalf("response carried no data\nbody: %s", r.Body)
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		t.Fatalf("could not decode data: %v\nbody: %s", err, r.Body)
	}
}

// Error returns the message the API reported, for asserting on failures.
func (r Response) Error(t *testing.T) string {
	t.Helper()

	var envelope struct {
		Errors string `json:"errors"`
	}
	_ = json.Unmarshal(r.Body, &envelope)
	return envelope.Errors
}

// Do sends a request through the real router. token may be empty.
func (h *Harness) Do(t *testing.T, method, path, token string, payload any) Response {
	return h.DoWithHeaders(t, method, path, token, payload, nil)
}

// DoWithHeaders is Do with extra request headers, for the handful of tests that
// care what the app does with headers a caller can set for themselves.
func (h *Harness) DoWithHeaders(t *testing.T, method, path, token string, payload any, headers map[string]string) Response {
	t.Helper()

	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("could not encode the request: %v", err)
		}
		body = bytes.NewReader(encoded)
	}

	request := httptest.NewRequest(method, path, body)
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", token)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}

	// -1 disables the timeout: some endpoints dial a mail server
	response, err := h.App.Test(request, -1)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, path, err)
	}
	defer response.Body.Close()

	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("could not read the response: %v", err)
	}

	return Response{Status: response.StatusCode, Body: raw}
}

func (h *Harness) Get(t *testing.T, path, token string) Response {
	return h.Do(t, http.MethodGet, path, token, nil)
}

func (h *Harness) Post(t *testing.T, path, token string, payload any) Response {
	return h.Do(t, http.MethodPost, path, token, payload)
}

func (h *Harness) Patch(t *testing.T, path, token string, payload any) Response {
	return h.Do(t, http.MethodPatch, path, token, payload)
}

func (h *Harness) Put(t *testing.T, path, token string, payload any) Response {
	return h.Do(t, http.MethodPut, path, token, payload)
}

func (h *Harness) Delete(t *testing.T, path, token string) Response {
	return h.Do(t, http.MethodDelete, path, token, nil)
}

// ---------------------------------------------------------------- factories

// Account is a registered, logged-in user.
type Account struct {
	ID    string
	Email string
	Token string
}

// Register creates a user through the public API, so the test exercises the
// same path a real signup takes.
func (h *Harness) Register(t *testing.T, email, password string) Account {
	t.Helper()

	created := h.Post(t, "/api/users", "", map[string]string{
		"email": email, "name": "Test " + email, "password": password,
	})
	if created.Status != fiber.StatusCreated {
		t.Fatalf("register %s failed: %d %s", email, created.Status, created.Body)
	}

	var user struct {
		ID string `json:"id"`
	}
	created.Decode(t, &user)

	return Account{ID: user.ID, Email: email, Token: h.Login(t, email, password)}
}

func (h *Harness) Login(t *testing.T, email, password string) string {
	t.Helper()

	response := h.Post(t, "/api/users/_login", "", map[string]string{
		"email": email, "password": password,
	})
	if response.Status != fiber.StatusOK {
		t.Fatalf("login %s failed: %d %s", email, response.Status, response.Body)
	}

	var login struct {
		Token string `json:"token"`
	}
	response.Decode(t, &login)

	return login.Token
}

// MakeSuperadmin grants the role directly, since there is deliberately no
// endpoint that lets a user promote themselves.
func (h *Harness) MakeSuperadmin(t *testing.T, userID string) {
	t.Helper()

	err := h.DB.Exec(`
		INSERT INTO user_roles (user_id, role_id, created_at)
		SELECT ?, id, 0 FROM roles WHERE slug = 'superadmin'
		ON CONFLICT DO NOTHING`, userID).Error
	if err != nil {
		t.Fatalf("could not grant superadmin: %v", err)
	}

	// roles ride along in the cached Auth, so the old token must go
	keys, _ := h.Redis.Keys(context.Background(), "user:token:*").Result()
	if len(keys) > 0 {
		h.Redis.Del(context.Background(), keys...)
	}
}

// MailAccountSettings points at the GreenMail server the dev stack runs.
func MailAccountSettings(host string, port int) map[string]any {
	return map[string]any{"host": host, "port": port, "use_tls": false}
}

func (h *Harness) MailHost() (string, int) {
	return h.Config.GetString("test.mail_host"), h.Config.GetInt("test.mail_port")
}

func (h *Harness) Truncate(t *testing.T, tables ...string) {
	t.Helper()
	for _, table := range tables {
		if err := h.DB.Exec(fmt.Sprintf("TRUNCATE %s CASCADE", table)).Error; err != nil {
			t.Fatalf("could not truncate %s: %v", table, err)
		}
	}
}
