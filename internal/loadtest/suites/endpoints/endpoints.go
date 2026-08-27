// Package endpoints load tests the HTTP surface.
//
// It drives real requests over the network against a running stack rather than
// fiber's in-process test helper. In-process would be faster and steadier, and
// that is the problem: it bypasses the connection pool, the server's own
// accept loop and the middleware ordering, which is half of what is being
// asked about here.
package endpoints

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"mailpulse/internal/entity"
	"mailpulse/internal/gateway/secret"
	"mailpulse/internal/loadtest/fixtures"
	"mailpulse/internal/loadtest/report"
	"mailpulse/internal/loadtest/runner"
)

type Suite struct {
	baseURL     string
	concurrency int
	duration    time.Duration
	pageSize    int
	only        string
	users       int
	accounts    int
	keep        bool
}

func New() *Suite { return &Suite{} }

func (s *Suite) Name() string  { return "endpoints" }
func (s *Suite) NeedsDB() bool { return true }
func (s *Suite) Synopsis() string {
	return "HTTP endpoint latency under concurrency, against a running stack"
}

func (s *Suite) Flags(fs *flag.FlagSet) {
	// 127.0.0.1 rather than localhost: inside the container localhost resolves
	// to ::1 first, and the server listens on IPv4 only
	fs.StringVar(&s.baseURL, "base-url", "http://127.0.0.1:3000", "the running API to load")
	fs.IntVar(&s.concurrency, "concurrency", 20, "simultaneous in-flight requests")
	fs.DurationVar(&s.duration, "duration", 15*time.Second, "how long to load each endpoint")
	fs.IntVar(&s.pageSize, "page-size", 100, "page size for list endpoints; admin-users is O(size) queries")
	fs.StringVar(&s.only, "endpoint", "", "run a single target by name instead of all")
	fs.IntVar(&s.users, "users", 500, "fixture users to create")
	fs.IntVar(&s.accounts, "accounts-per-user", 2, "fixture mail accounts per user")
	fs.BoolVar(&s.keep, "keep", false, "leave the fixtures behind for inspection")
}

// target is one endpoint worth loading, and why.
type target struct {
	name   string
	path   string
	method string
	body   any
	// auth false means the request is deliberately unauthenticated, which is
	// how the login endpoint is measured
	auth bool
	note string
}

func (s *Suite) Run(ctx context.Context, env *runner.Env) (*report.Result, error) {
	cipher, err := secret.NewCipher(env.Config.GetString("security.encryption_key"))
	if err != nil {
		return nil, err
	}

	result := &report.Result{
		Suite:     s.Name(),
		StartedAt: time.Now(),
		GitSHA:    report.GitSHA(),
		Params: map[string]any{
			"base_url": s.baseURL, "concurrency": s.concurrency,
			"duration": s.duration.String(), "page_size": s.pageSize,
			"users": s.users, "accounts_per_user": s.accounts,
		},
	}

	env.Log.Infof("Building fixtures: %d users, %d accounts each", s.users, s.accounts)

	built, err := fixtures.Build(ctx, env.DB, cipher, fixtures.Shape{
		Users:              s.users,
		AccountsPerUser:    s.accounts,
		WatchersPerAccount: 2,
		FiltersPerWatcher:  3,
		EventsPerWatcher:   1,
		Provider:           "imap",
	}, env.Seed)
	if err != nil {
		return nil, fmt.Errorf("building fixtures: %w", err)
	}

	if !s.keep {
		defer func() {
			if err := fixtures.Cleanup(context.WithoutCancel(ctx), env.DB); err != nil {
				env.Log.WithError(err).Error("Fixture cleanup failed; rows are still in the database")
			}
		}()
	}

	// the admin endpoints are the interesting ones, and they need the role
	if err := s.promote(ctx, env, built.UserIDs[0]); err != nil {
		return nil, fmt.Errorf("promoting the fixture admin: %w", err)
	}

	loginEmail := fmt.Sprintf("user-%06d@%s", 0, fixtures.EmailDomain)
	token, err := s.login(ctx, loginEmail, built.Password)
	if err != nil {
		return nil, fmt.Errorf("logging the fixture admin in: %w", err)
	}

	for _, t := range s.targets(loginEmail, built.Password) {
		if s.only != "" && s.only != t.name {
			continue
		}

		env.Log.Infof("Loading %s for %s at concurrency %d", t.name, s.duration, s.concurrency)

		client := &http.Client{Timeout: 60 * time.Second}
		var statuses sync4xx

		stats := runner.Drive(ctx, runner.Plan{
			Concurrency: s.concurrency,
			Duration:    s.duration,
		}, func(ctx context.Context, _ int) error {
			status, err := s.call(ctx, client, t, token)
			statuses.record(status)
			return err
		})

		extra := map[string]any{}
		if bad := statuses.bad(); len(bad) > 0 {
			extra["non_2xx"] = bad
		}
		if t.note != "" {
			extra["note"] = t.note
		}
		result.Add(t.name, stats, extra)
	}

	result.Elapsed = time.Since(result.StartedAt)
	s.conclude(result)

	return result, nil
}

func (s *Suite) targets(email, password string) []target {
	page := fmt.Sprintf("?page=1&size=%d", s.pageSize)

	return []target{
		{
			name: "admin-users", path: "/api/admin/users" + page, method: http.MethodGet, auth: true,
			note: "ListUsers calls countsFor inside the row loop: four count queries per row",
		},
		{name: "dashboard-summary", path: "/api/dashboard/summary", method: http.MethodGet, auth: true},
		{name: "matches", path: "/api/matches" + page, method: http.MethodGet, auth: true},
		{name: "event-runs", path: "/api/event-runs" + page, method: http.MethodGet, auth: true},
		{name: "mail-accounts", path: "/api/mail-accounts" + page, method: http.MethodGet, auth: true},
		{
			name: "auth-middleware", path: "/api/users/_current", method: http.MethodGet, auth: true,
			note: "the cheapest authenticated route: a floor under everything above",
		},
		{
			name: "login", path: "/api/users/_login", method: http.MethodPost, auth: false,
			body: map[string]string{"email": email, "password": password},
			note: "bcrypt is deliberately expensive; fixtures use MinCost so this is a floor, not production cost",
		},
	}
}

func (s *Suite) call(ctx context.Context, client *http.Client, t target, token string) (int, error) {
	var body io.Reader
	if t.body != nil {
		encoded, err := json.Marshal(t.body)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, t.method, s.baseURL+t.path, body)
	if err != nil {
		return 0, err
	}
	if t.body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if t.auth {
		request.Header.Set("Authorization", token)
	}

	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()

	// the body has to be drained or the connection is not reused, and a load
	// test that opens a new socket per request measures the dialer
	_, _ = io.Copy(io.Discard, response.Body)

	if response.StatusCode >= 400 {
		return response.StatusCode, fmt.Errorf("%s answered %d", t.name, response.StatusCode)
	}

	return response.StatusCode, nil
}

func (s *Suite) login(ctx context.Context, email, password string) (string, error) {
	encoded, err := json.Marshal(map[string]string{"email": email, "password": password})
	if err != nil {
		return "", err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.baseURL+"/api/users/_login", bytes.NewReader(encoded))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		return "", fmt.Errorf("is the stack running at %s? %w", s.baseURL, err)
	}
	defer response.Body.Close()

	payload, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login answered %d: %s", response.StatusCode, strings.TrimSpace(string(payload)))
	}

	var decoded struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", err
	}
	if decoded.Data.Token == "" {
		return "", fmt.Errorf("login returned no token: %s", string(payload))
	}

	return decoded.Data.Token, nil
}

// promote gives the first fixture user the superadmin role by writing the join
// row directly. Roles are a pivot with fixed seeded ids, so this looks up the
// row rather than hard-coding the uuid the seeder happens to use.
func (s *Suite) promote(ctx context.Context, env *runner.Env, userID string) error {
	var role entity.Role
	if err := env.DB.WithContext(ctx).Where("slug = ?", entity.RoleSuperadmin).Take(&role).Error; err != nil {
		return fmt.Errorf("no %s role is seeded, run make seed: %w", entity.RoleSuperadmin, err)
	}

	// the pivot's key is (user_id, role_id) — it carries no id of its own
	return env.DB.WithContext(ctx).Exec(
		"INSERT INTO user_roles (user_id, role_id, created_at) VALUES (?, ?, ?) ON CONFLICT DO NOTHING",
		userID, role.ID, time.Now().UnixMilli()).Error
}

// conclude turns the table into the sentence a reader wants, by naming the
// slowest target rather than leaving them to scan the p95 column.
func (s *Suite) conclude(result *report.Result) {
	var worst *report.Metric
	for i := range result.Metrics {
		if worst == nil || result.Metrics[i].Stats.P95 > worst.Stats.P95 {
			worst = &result.Metrics[i]
		}
	}
	if worst == nil {
		return
	}

	result.Findf("slowest at p95: %s (%s) at concurrency %d",
		worst.Name, worst.Stats.P95.Round(time.Millisecond), s.concurrency)

	for _, m := range result.Metrics {
		if m.Stats.Errors == 0 {
			continue
		}

		// a 429 is the rate limiter doing its job, not a failure, and it
		// means the timings for that target measure the limiter rather than
		// the handler behind it. Saying so is the difference between a
		// misleading number and a useful one.
		if throttled, ok := rateLimited(m.Extra); ok {
			result.Findf("%s was rate-limited on %d of %d requests: these timings are the limiter, not the endpoint. Load it alone with -endpoint=%s -concurrency=1",
				m.Name, throttled, m.Stats.Count, m.Name)
			continue
		}

		result.Findf("%s returned %d errors out of %d requests",
			m.Name, m.Stats.Errors, m.Stats.Count)
	}
}

// rateLimited pulls the 429 count out of a metric's recorded statuses.
func rateLimited(extra map[string]any) (int, bool) {
	statuses, ok := extra["non_2xx"].(map[int]int)
	if !ok {
		return 0, false
	}

	count, ok := statuses[429]
	return count, ok && count > 0
}
