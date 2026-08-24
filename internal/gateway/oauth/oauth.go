// Package oauth exchanges authorization codes for mailbox tokens.
//
// It is deliberately provider-shaped rather than mail-shaped: nothing here
// knows about IMAP, mail_accounts or the cipher. The usecase layer decides what
// to store and how to encrypt it, exactly as it does for the mail gateway.
package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// ErrRevoked reports that the user took our access away: the refresh token no
// longer works and no amount of retrying will change that. The caller is
// expected to park the account and ask for consent again rather than back off.
var ErrRevoked = errors.New("oauth: the user revoked access, the mailbox must be reconnected")

// Tokens is what a successful exchange or refresh yields.
//
// ExpiresAt is epoch milliseconds, matching mail_accounts.token_expires_at and
// every other timestamp in the schema.
type Tokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64
	Scopes       []string
	// IDToken is present when the provider speaks OIDC. It is read once, to
	// learn which mailbox was connected, and deliberately never stored: it
	// says who the user is, which we already know, and it cannot be spent.
	IDToken string
}

// Identity is who the tokens belong to. The callback needs it because the
// consent screen, not our form, decides which mailbox was connected.
type Identity struct {
	Email     string
	AccountID string
}

// Client is one provider's half of the flow.
type Client interface {
	Slug() string
	AuthorizeURL(state string) string
	Exchange(ctx context.Context, code string) (Tokens, error)
	Refresh(ctx context.Context, refreshToken string) (Tokens, error)
	Identify(ctx context.Context, tokens Tokens) (Identity, error)
}

// Config is the per-provider half that comes from the environment.
type Config struct {
	ClientID     string
	ClientSecret string
	// Tenant is Microsoft only; "common" lets any work or personal account in.
	Tenant string
}

// Configured reports whether the environment actually supplied this provider.
// An unconfigured provider is never registered, which is what keeps the 501 at
// _authorize honest instead of failing later at the consent screen.
func (c Config) Configured() bool {
	return c.ClientID != "" && c.ClientSecret != ""
}

// identifier works out whose mailbox was just connected. Providers disagree on
// both the endpoint and the field names, and Microsoft has no endpoint our
// access token is even addressed to, so the whole job lives with the provider.
type identifier func(ctx context.Context, tokens Tokens) (Identity, error)

// client is the shared implementation. Providers differ only in endpoints,
// scopes, the extra parameters on the consent URL, and how identity is read,
// so there is one exchange and one refresh rather than three copies.
type client struct {
	slug       string
	config     *oauth2.Config
	authParams []oauth2.AuthCodeOption
	identify   identifier
}

func (c *client) Slug() string { return c.slug }

func (c *client) AuthorizeURL(state string) string {
	return c.config.AuthCodeURL(state, c.authParams...)
}

func (c *client) Exchange(ctx context.Context, code string) (Tokens, error) {
	token, err := c.config.Exchange(ctx, code)
	if err != nil {
		return Tokens{}, translate(err)
	}
	return convert(token, c.config.Scopes), nil
}

// Refresh spends a refresh token. x/oauth2 only refreshes when it thinks the
// access token has expired, so the seed token is given a zero expiry to force
// the round trip.
func (c *client) Refresh(ctx context.Context, refreshToken string) (Tokens, error) {
	if refreshToken == "" {
		return Tokens{}, fmt.Errorf("%w: no refresh token is stored", ErrRevoked)
	}

	source := c.config.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken})

	token, err := source.Token()
	if err != nil {
		return Tokens{}, translate(err)
	}

	tokens := convert(token, c.config.Scopes)

	// Google returns no refresh token on a refresh, only on first consent.
	// Carrying the old one forward keeps the caller from having to know that.
	if tokens.RefreshToken == "" {
		tokens.RefreshToken = refreshToken
	}

	return tokens, nil
}

func (c *client) Identify(ctx context.Context, tokens Tokens) (Identity, error) {
	if tokens.AccessToken == "" {
		return Identity{}, errors.New("oauth: cannot identify an account without an access token")
	}
	return c.identify(ctx, tokens)
}

func convert(token *oauth2.Token, fallback []string) Tokens {
	tokens := Tokens{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Scopes:       fallback,
	}

	if !token.Expiry.IsZero() {
		tokens.ExpiresAt = token.Expiry.UnixMilli()
	}

	// the granted scopes can be narrower than the ones asked for, and that is
	// worth recording: it is the difference between "we asked for mail" and
	// "the user agreed to mail"
	if granted, ok := token.Extra("scope").(string); ok && granted != "" {
		tokens.Scopes = strings.Fields(granted)
	}

	if id, ok := token.Extra("id_token").(string); ok {
		tokens.IDToken = id
	}

	return tokens
}

// translate turns the one provider error that means something specific into
// ErrRevoked, and leaves everything else alone. invalid_grant is returned for a
// refresh token the user has revoked, one issued to a deleted account, and one
// that simply aged out — all of which need the same answer: reconnect.
func translate(err error) error {
	var retrieve *oauth2.RetrieveError
	if errors.As(err, &retrieve) && retrieve.ErrorCode == "invalid_grant" {
		return fmt.Errorf("%w: %s", ErrRevoked, retrieve.ErrorDescription)
	}
	return err
}

// getJSON reads a JSON document behind an Authorization header. The header
// value is passed whole rather than assembled here because Yandex answers to
// the "OAuth" scheme and everyone else to "Bearer". The body is capped: a
// userinfo endpoint answering with something enormous is a reason to stop, not
// to buffer it.
func getJSON(ctx context.Context, url, authorization string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", authorization)
	request.Header.Set("Accept", "application/json")

	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		return fmt.Errorf("oauth: cannot reach %s: %w", url, err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("oauth: cannot read the response from %s: %w", url, err)
	}

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("oauth: %s answered %d", url, response.StatusCode)
	}

	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("oauth: %s returned something that is not JSON: %w", url, err)
	}

	return nil
}

// idTokenClaims reads the payload of an OIDC id_token.
//
// The signature is deliberately not checked. This token came back on our own
// TLS connection to the provider's token endpoint, in response to a code we
// just minted — OIDC Core 3.1.3.7 allows skipping validation on that path, and
// there is no third party in between whose word we would be taking. Nothing
// security-bearing is decided from these claims either: the result names a
// mailbox, and the account it lands on is fixed by the state we issued.
func idTokenClaims(raw string) (map[string]any, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return nil, errors.New("oauth: the id_token is not a JWT")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("oauth: the id_token payload is not base64url: %w", err)
	}

	claims := map[string]any{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("oauth: the id_token payload is not JSON: %w", err)
	}

	return claims, nil
}

// claim returns the first of names that holds a non-empty string.
func claim(claims map[string]any, names ...string) string {
	for _, name := range names {
		if value, ok := claims[name].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

// Registry holds the providers the environment configured, keyed by
// mail_providers.slug.
//
// It is keyed by slug rather than by kind, unlike the mail registry: which
// consent screen to send a user to is a property of the service, not of the
// transport, and gmail and outlook share the imap kind.
type Registry struct {
	clients map[string]Client
}

func NewRegistry() *Registry {
	return &Registry{clients: map[string]Client{}}
}

func (r *Registry) Register(clients ...Client) {
	for _, client := range clients {
		if client != nil {
			r.clients[client.Slug()] = client
		}
	}
}

func (r *Registry) Has(slug string) bool {
	_, ok := r.clients[slug]
	return ok
}

func (r *Registry) Get(slug string) (Client, bool) {
	client, ok := r.clients[slug]
	return client, ok
}

// Slugs lists what is configured, for logging at startup.
func (r *Registry) Slugs() []string {
	slugs := make([]string, 0, len(r.clients))
	for slug := range r.clients {
		slugs = append(slugs, slug)
	}
	return slugs
}

// CallbackURL is the redirect_uri registered with the provider. It has to match
// what the provider has on file byte for byte, which is why it is built in one
// place from one setting rather than assembled at each call site.
func CallbackURL(base, slug string) string {
	return strings.TrimRight(base, "/") + "/api/mail-accounts/oauth/" + slug + "/_callback"
}
