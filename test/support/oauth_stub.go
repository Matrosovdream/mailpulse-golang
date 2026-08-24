//go:build integration || feature

package support

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
)

// OAuthStub is a mail provider's OAuth endpoints, under the test's control.
//
// It answers /token and /userinfo; /authorize is never called, because that is
// the page a browser would be sent to and no test follows a redirect off our
// own host. The application talks to it through the real oauth client, so the
// exchange, the refresh and the storage under test are the production ones.
type OAuthStub struct {
	Server *httptest.Server

	mu sync.Mutex

	// what /token returns
	accessToken  string
	refreshToken string
	expiresIn    int
	scope        string
	tokenStatus  int

	// what /userinfo returns
	email   string
	subject string

	// how many times each endpoint was called, so a test can prove that N
	// concurrent syncs caused exactly one refresh
	exchanges int
	refreshes int
}

func newOAuthStub() *OAuthStub {
	stub := &OAuthStub{}
	stub.reset()

	mux := http.NewServeMux()
	mux.HandleFunc("/token", stub.token)
	mux.HandleFunc("/userinfo", stub.userinfo)

	stub.Server = httptest.NewServer(mux)

	return stub
}

// reset returns the stub to a working default. Held separately from Reset so
// the constructor can use it before the server exists.
func (s *OAuthStub) reset() {
	s.accessToken = "stub-access-token"
	s.refreshToken = "stub-refresh-token"
	s.expiresIn = 3600
	s.scope = "mail:imap_ro login:email"
	s.tokenStatus = http.StatusOK
	s.email = "connected@yandex.com"
	s.subject = "stub-subject-1"
	s.exchanges = 0
	s.refreshes = 0
}

// Reset puts the stub back to its defaults between tests.
func (s *OAuthStub) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reset()
}

// Configure changes one or more of the stub's answers. A nil field is left
// alone, which is what lets a test say "same as before, but no refresh token".
func (s *OAuthStub) Configure(change func(c *StubConfig)) {
	s.mu.Lock()
	defer s.mu.Unlock()

	config := StubConfig{
		AccessToken:  s.accessToken,
		RefreshToken: s.refreshToken,
		ExpiresIn:    s.expiresIn,
		Scope:        s.scope,
		TokenStatus:  s.tokenStatus,
		Email:        s.email,
		Subject:      s.subject,
	}

	change(&config)

	s.accessToken = config.AccessToken
	s.refreshToken = config.RefreshToken
	s.expiresIn = config.ExpiresIn
	s.scope = config.Scope
	s.tokenStatus = config.TokenStatus
	s.email = config.Email
	s.subject = config.Subject
}

// StubConfig is the mutable half of the stub, handed to Configure.
type StubConfig struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
	Scope        string
	// TokenStatus forces /token to fail. Pair it with 400 and the oauth
	// library reads the body as an error, which is how invalid_grant is
	// simulated.
	TokenStatus int
	Email       string
	Subject     string
}

// Counts reports how many exchanges and refreshes the stub has served.
func (s *OAuthStub) Counts() (exchanges int, refreshes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exchanges, s.refreshes
}

func (s *OAuthStub) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()

	s.mu.Lock()
	if r.Form.Get("grant_type") == "refresh_token" {
		s.refreshes++
	} else {
		s.exchanges++
	}
	status := s.tokenStatus
	body := map[string]any{
		"access_token": s.accessToken,
		"token_type":   "Bearer",
		"expires_in":   s.expiresIn,
		"scope":        s.scope,
	}
	if s.refreshToken != "" {
		body["refresh_token"] = s.refreshToken
	}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")

	if status != http.StatusOK {
		w.WriteHeader(status)
		// the shape a provider uses to say the grant is dead, which is what
		// the resolver keys on to park the account instead of retrying
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_grant",
			"error_description": "the token has been revoked",
		})
		return
	}

	_ = json.NewEncoder(w).Encode(body)
}

func (s *OAuthStub) userinfo(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	body := map[string]string{"sub": s.subject, "email": s.email}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
