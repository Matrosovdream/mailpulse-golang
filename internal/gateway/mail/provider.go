package mail

import (
	"context"
	"encoding/json"
	"fmt"

	"mailpulse/internal/model"

	"github.com/gofiber/fiber/v2"
)

// Provider kinds. A kind is one client implementation; several provider slugs
// (yandex, mailru, fastmail) can share one, which is why mail_providers.kind
// and mail_providers.slug are separate columns.
const (
	KindIMAP         = "imap"
	KindGmailAPI     = "gmail_api"
	KindGraphAPI     = "graph_api"
	KindInboundRelay = "inbound_relay"
)

// Auth modes. Transport and authentication are independent axes: xoauth2 is
// IMAP transport carrying an OAuth token, which is why this cannot collapse
// back into the provider kind.
const (
	AuthPassword    = "password"
	AuthAppPassword = "app_password"
	AuthOAuth2      = "oauth2"
	AuthXOAuth2     = "xoauth2"
)

// Credentials are decrypted by the usecase and handed down. The gateway never
// sees the cipher or the database.
type Credentials struct {
	Username     string `json:"username,omitempty"`
	Password     string `json:"password,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
}

// Account is the provider-agnostic view of a mail_accounts row.
//
// Settings is deliberately opaque: an IMAP client reads host and port from it,
// a Gmail client reads nothing, and neither has to carry the other's columns.
type Account struct {
	ID           string
	Provider     string // mail_providers.slug
	Kind         string // mail_providers.kind
	EmailAddress string
	AuthMode     string
	Settings     json.RawMessage
	Credentials  Credentials
}

// DecodeSettings unpacks the provider-specific blob into a typed struct.
func (a Account) DecodeSettings(target any) error {
	if len(a.Settings) == 0 {
		return nil
	}
	return json.Unmarshal(a.Settings, target)
}

type Folder struct {
	Name         string `json:"name"`
	MessageCount int    `json:"message_count"`
}

// Message is one fetched email, flattened to the parts the filters can test.
type Message struct {
	MessageID       string
	UID             string
	Subject         string
	FromAddress     string
	FromName        string
	To              []string
	Cc              []string
	BodyText        string
	Headers         map[string]string
	HasAttachment   bool
	AttachmentNames []string
	SizeBytes       int
	ReceivedAt      int64
}

type FetchRequest struct {
	Folder string
	Since  int64
	Cursor []byte
	Limit  int
}

type FetchResult struct {
	Messages []Message
	Cursor   []byte
}

// Capabilities lets the API and the SPA reason about a connection without
// hard-coding which providers can do what.
type Capabilities struct {
	Folders      bool `json:"folders"`
	Labels       bool `json:"labels"`
	Push         bool `json:"push"`
	Idle         bool `json:"idle"`
	ServerSearch bool `json:"server_search"`
}

// Descriptor is everything the API needs to describe a provider implementation.
type Descriptor struct {
	Kind         string       `json:"kind"`
	Label        string       `json:"label"`
	AuthModes    []string     `json:"auth_modes"`
	Capabilities Capabilities `json:"capabilities"`
	ConfigSchema model.Schema `json:"config_schema"`
}

// Provider is the minimum every mail client implements.
//
// Anything optional is a separate interface below, discovered by type
// assertion, so an API provider is never forced to stub IMAP concepts.
type Provider interface {
	Describe() Descriptor
	Verify(ctx context.Context, account Account) ([]Folder, error)
	Fetch(ctx context.Context, account Account, request FetchRequest) (FetchResult, error)
}

// FolderLister is implemented by providers that can enumerate mailboxes.
type FolderLister interface {
	Folders(ctx context.Context, account Account) ([]Folder, error)
}

// Subscription is a push registration held on the provider's side.
type Subscription struct {
	ProviderSubscriptionID string
	Resource               string
	ClientSecret           string
	ExpiresAt              int64
}

// Notification identifies which account a push callback belongs to.
type Notification struct {
	ProviderSubscriptionID string
	AccountHint            string
}

// PushProvider is implemented by providers that can notify us instead of being
// polled. Not implemented by anything yet; declared so the worker and the
// mail_push_subscriptions table have a contract to build against.
type PushProvider interface {
	Subscribe(ctx context.Context, account Account, callbackURL string) (Subscription, error)
	Renew(ctx context.Context, account Account, subscription Subscription) (Subscription, error)
	Unsubscribe(ctx context.Context, account Account, subscription Subscription) error
	ParseNotification(ctx context.Context, body []byte, headers map[string]string) (Notification, error)
}

// TokenRefresher is implemented by providers whose credentials expire.
type TokenRefresher interface {
	Refresh(ctx context.Context, account Account) (Credentials, error)
}

type Registry struct {
	providers map[string]Provider
}

func NewRegistry() *Registry {
	return &Registry{providers: map[string]Provider{}}
}

func (r *Registry) Register(providers ...Provider) {
	for _, provider := range providers {
		r.providers[provider.Describe().Kind] = provider
	}
}

// Get looks a provider up by kind, not by slug: the slug lives in the database
// so a new integration is a row, and the row names the kind that serves it.
func (r *Registry) Get(kind string) (Provider, error) {
	provider, ok := r.providers[kind]
	if !ok {
		return nil, fiber.NewError(fiber.StatusBadRequest,
			fmt.Sprintf("no mail provider is registered for %q", kind))
	}
	return provider, nil
}

func (r *Registry) Has(kind string) bool {
	_, ok := r.providers[kind]
	return ok
}

// Descriptors backs the catalog endpoint, keyed by kind.
func (r *Registry) Descriptors() map[string]Descriptor {
	out := make(map[string]Descriptor, len(r.providers))
	for kind, provider := range r.providers {
		out[kind] = provider.Describe()
	}
	return out
}

// ListFolders calls the optional FolderLister, and says so plainly when the
// provider does not enumerate mailboxes.
func ListFolders(ctx context.Context, provider Provider, account Account) ([]Folder, error) {
	lister, ok := provider.(FolderLister)
	if !ok {
		return nil, fiber.NewError(fiber.StatusNotImplemented,
			"this provider does not list folders")
	}
	return lister.Folders(ctx, account)
}
