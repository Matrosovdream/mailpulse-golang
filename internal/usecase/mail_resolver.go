package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"mailpulse/internal/entity"
	"mailpulse/internal/gateway/cache"
	"mailpulse/internal/gateway/mail"
	"mailpulse/internal/gateway/oauth"
	"mailpulse/internal/gateway/secret"
	"mailpulse/internal/model"
	"mailpulse/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// refreshWindow is how far ahead of expiry a token is renewed. It has to
// comfortably exceed the longest sync a token might be used for, or a token
// that was valid at connect time expires mid-fetch.
const refreshWindow = 5 * time.Minute

// refreshLockTTL bounds how long one worker may hold the right to refresh an
// account before another is allowed to try. Longer than the provider round
// trip, short enough that a crashed worker does not strand the mailbox.
const refreshLockTTL = 30 * time.Second

// refreshLockScope namespaces the lock keys.
const refreshLockScope = "oauth:refresh"

// MailResolver turns a stored mail_accounts row into the client that serves it
// plus the provider-agnostic view that client expects.
//
// It exists so the account usecase and the sync pipeline share one path:
// look up the provider row for its kind, find the registered client, decrypt
// the credentials. Decryption happens here, in the usecase layer — the gateway
// never sees the cipher.
//
// Renewing an expiring OAuth token happens here too, rather than in a separate
// method the callers opt into. There are four ways into a mailbox — Verify,
// Folders, the re-check job and the sync pipeline — and a token refresh that
// each of them has to remember is one forgotten call site away from a mailbox
// that stops working an hour after it is connected. Same reasoning as
// impersonation being resolved centrally: there is no second door.
type MailResolver struct {
	// DB is the root handle, deliberately not the caller's transaction. A
	// renewed token has to be committed on its own: providers invalidate the
	// old refresh token the moment they issue a new one, so a refresh that
	// gets rolled back with the caller's work leaves the account holding a
	// credential the provider has already retired — unrecoverable without the
	// user consenting again. The same goes for parking a revoked account.
	DB       *gorm.DB
	Rows     *repository.MailProviderRepository
	Accounts *repository.MailAccountRepository
	Registry *mail.Registry
	OAuth    *oauth.Registry
	Cipher   *secret.Cipher
	Lock     *cache.Lock
	Log      *logrus.Logger

	// Providers caches the mail_providers row per slug. Only this path uses
	// it: the poller resolves every account on every cycle, so the same seven
	// rows are read thousands of times a minute. The HTTP write paths in
	// MailAccountUseCase deliberately still read through, so an edited
	// provider takes effect there immediately.
	Providers *cache.MailProviderCache
}

func NewMailResolver(db *gorm.DB, rows *repository.MailProviderRepository,
	accounts *repository.MailAccountRepository, registry *mail.Registry,
	oauthClients *oauth.Registry, cipher *secret.Cipher,
	lock *cache.Lock, providers *cache.MailProviderCache, log *logrus.Logger) *MailResolver {
	return &MailResolver{
		DB: db, Rows: rows, Accounts: accounts, Registry: registry,
		OAuth: oauthClients, Cipher: cipher, Lock: lock,
		Providers: providers, Log: log,
	}
}

// Row returns the mail_providers row for a slug, through the cache.
//
// A cache that is unreachable, or holds nothing yet, falls through to the
// database rather than reporting the provider as unknown — the failure mode of
// getting that backwards is every mailbox in the fleet parked at once.
func (r *MailResolver) Row(ctx context.Context, db *gorm.DB, slug string) (*entity.MailProvider, error) {
	if r.Providers != nil {
		if row, found, cached := r.Providers.Lookup(ctx, slug); cached {
			if !found {
				return nil, unknownProvider(slug)
			}
			return row, nil
		}
	}

	row := new(entity.MailProvider)
	if err := r.Rows.FindBySlug(db, row, slug); err != nil {
		if r.Providers != nil {
			// remember the miss too: an unknown slug is a 400, and a client
			// retrying one would otherwise reach the database every time
			r.Providers.Remember(ctx, slug, nil)
		}
		return nil, unknownProvider(slug)
	}

	if r.Providers != nil {
		r.Providers.Remember(ctx, slug, row)
	}

	return row, nil
}

func unknownProvider(slug string) error {
	return fiber.NewError(fiber.StatusBadRequest,
		fmt.Sprintf("unknown mail provider %q", slug))
}

// Resolve prepares an account for use, renewing its access token first when one
// is about to expire. The account struct is updated in place, so a caller that
// saves it afterwards writes the refreshed credentials rather than the stale
// ones it read.
func (r *MailResolver) Resolve(ctx context.Context, db *gorm.DB, account *entity.MailAccount) (mail.Provider, mail.Account, error) {
	row, err := r.Row(ctx, db, account.Provider)
	if err != nil {
		return nil, mail.Account{}, err
	}

	client, err := r.Registry.Get(row.Kind)
	if err != nil {
		return nil, mail.Account{}, err
	}

	if err := r.refresh(ctx, account); err != nil {
		return nil, mail.Account{}, err
	}

	target, err := r.Account(account, row)
	if err != nil {
		return nil, mail.Account{}, err
	}

	return client, target, nil
}

func (r *MailResolver) Account(account *entity.MailAccount, row *entity.MailProvider) (mail.Account, error) {
	credentials, err := r.Credentials(account)
	if err != nil {
		return mail.Account{}, err
	}

	return mail.Account{
		ID:           account.ID,
		Provider:     account.Provider,
		Kind:         row.Kind,
		EmailAddress: account.EmailAddress,
		AuthMode:     account.AuthMode,
		Settings:     json.RawMessage(entity.JSONOrEmpty(account.Settings, "{}")),
		Credentials: mail.Credentials{
			Username:     credentials.Username,
			Password:     credentials.Password,
			AccessToken:  credentials.AccessToken,
			RefreshToken: credentials.RefreshToken,
			ExpiresAt:    int64OrZero(account.TokenExpiresAt),
		},
	}, nil
}

func (r *MailResolver) Credentials(account *entity.MailAccount) (model.MailAccountCredentials, error) {
	var credentials model.MailAccountCredentials

	plaintext, err := r.Cipher.Decrypt(account.Credentials)
	if err != nil {
		return credentials, fiber.NewError(fiber.StatusInternalServerError,
			"stored credentials could not be decrypted")
	}

	if plaintext != "" {
		_ = json.Unmarshal([]byte(plaintext), &credentials)
	}

	return credentials, nil
}

func (r *MailResolver) Encrypt(credentials model.MailAccountCredentials) (string, error) {
	encoded, err := credentials.Encode()
	if err != nil {
		return "", fiber.ErrInternalServerError
	}

	encrypted, err := r.Cipher.Encrypt(encoded)
	if err != nil {
		return "", fiber.ErrInternalServerError
	}

	return encrypted, nil
}

// needsRefresh reports whether the account holds an OAuth token that is close
// enough to expiry to renew.
//
// A nil token_expires_at means no refresh: either this is a password account,
// or it is an OAuth one whose expiry we never learned, and renewing on every
// poll because we cannot tell is exactly the refresh storm a 30 second poll
// interval makes expensive.
func needsRefresh(account *entity.MailAccount) bool {
	if account.AuthMode != mail.AuthXOAuth2 && account.AuthMode != mail.AuthOAuth2 {
		return false
	}
	if account.TokenExpiresAt == nil {
		return false
	}
	return time.Now().Add(refreshWindow).UnixMilli() >= *account.TokenExpiresAt
}

// refresh renews an expiring access token, once, across however many workers
// are looking at the same account.
//
// Every read and write below goes through r.DB rather than the caller's
// transaction, for the reason given on the field.
func (r *MailResolver) refresh(ctx context.Context, account *entity.MailAccount) error {
	if !needsRefresh(account) {
		return nil
	}

	db := r.DB.WithContext(ctx)

	client, ok := r.OAuth.Get(account.Provider)
	if !ok {
		return fiber.NewError(fiber.StatusFailedDependency,
			"this mailbox was connected with OAuth but "+account.Provider+
				" is no longer configured on the server, so its token cannot be renewed")
	}

	// Someone else got here first. Wait briefly for their write rather than
	// duplicating the exchange, then use whatever is stored — including if it
	// is still stale, in which case this connection fails and the next poll
	// tries again. Waiting forever would be worse than one failed poll.
	if !r.Lock.Acquire(ctx, refreshLockScope, account.ID, refreshLockTTL) {
		r.awaitRefresh(ctx, db, account)
		return nil
	}
	defer r.Lock.Release(ctx, refreshLockScope, account.ID)

	// the lock was contended a moment ago; re-read before spending the refresh
	// token in case the winner finished between the two calls
	if fresh := r.reload(db, account.ID); fresh != nil && !needsRefresh(fresh) {
		adopt(account, fresh)
		return nil
	}

	credentials, err := r.Credentials(account)
	if err != nil {
		return err
	}

	tokens, err := client.Refresh(ctx, credentials.RefreshToken)
	if err != nil {
		return r.handleRefreshFailure(db, account, err)
	}

	return r.store(db, account, credentials, tokens)
}

// awaitRefresh polls for the winner's write for a bounded time.
func (r *MailResolver) awaitRefresh(ctx context.Context, db *gorm.DB, account *entity.MailAccount) {
	const attempts = 8
	const interval = 250 * time.Millisecond

	for i := 0; i < attempts; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}

		fresh := r.reload(db, account.ID)
		if fresh == nil {
			return
		}
		if !needsRefresh(fresh) {
			adopt(account, fresh)
			return
		}
	}

	r.Log.Warnf("Timed out waiting for another worker to refresh the token for %s", account.EmailAddress)
}

func (r *MailResolver) reload(db *gorm.DB, id string) *entity.MailAccount {
	fresh := new(entity.MailAccount)
	if err := r.Accounts.FindById(db, fresh, id); err != nil {
		return nil
	}
	return fresh
}

// adopt copies the credential fields another worker just wrote onto the row we
// are holding, leaving everything else alone: the caller may have pending
// changes of its own, such as a sync cursor.
func adopt(account *entity.MailAccount, fresh *entity.MailAccount) {
	account.Credentials = fresh.Credentials
	account.TokenExpiresAt = fresh.TokenExpiresAt
	account.Scopes = fresh.Scopes
}

// store writes renewed tokens back, merging into the existing credentials
// rather than rebuilding them.
func (r *MailResolver) store(db *gorm.DB, account *entity.MailAccount,
	credentials model.MailAccountCredentials, tokens oauth.Tokens) error {

	credentials.AccessToken = tokens.AccessToken

	// An empty refresh token means "keep using the one you have", which is what
	// Google returns on every refresh. Assigning it unconditionally would strip
	// the account's only way to renew, and the failure would surface an hour
	// later looking like a revoked grant.
	if tokens.RefreshToken != "" {
		credentials.RefreshToken = tokens.RefreshToken
	}

	encrypted, err := r.Encrypt(credentials)
	if err != nil {
		return err
	}

	account.Credentials = encrypted
	if tokens.ExpiresAt > 0 {
		account.TokenExpiresAt = &tokens.ExpiresAt
	}
	if len(tokens.Scopes) > 0 {
		account.Scopes = ptr(strings.Join(tokens.Scopes, " "))
	}

	if err := r.Accounts.Update(db, account); err != nil {
		r.Log.WithError(err).Warnf("Failed to store a refreshed token for %s", account.EmailAddress)
		return fiber.ErrInternalServerError
	}

	return nil
}

// handleRefreshFailure tells a revoked grant apart from a bad day.
//
// invalid_grant is terminal: the user withdrew consent, or the token aged out
// past the provider's limit. Retrying cannot fix it and every retry is a
// request the provider counts against us, so the account is parked with an
// explanation the dashboard can show. Anything else is transient and the next
// poll tries again.
func (r *MailResolver) handleRefreshFailure(db *gorm.DB, account *entity.MailAccount, err error) error {
	if !errors.Is(err, oauth.ErrRevoked) {
		r.Log.WithError(err).Warnf("Could not renew the token for %s, will retry", account.EmailAddress)
		return fiber.NewError(fiber.StatusBadGateway,
			"could not renew the access token for this mailbox: "+err.Error())
	}

	message := "access to this mailbox was withdrawn; reconnect it to grant it again"

	account.Status = entity.MailAccountStatusError
	account.LastError = &message

	if updateErr := r.Accounts.Update(db, account); updateErr != nil {
		r.Log.WithError(updateErr).Warnf("Failed to park the revoked account %s", account.ID)
	}

	r.Log.Warnf("OAuth access for %s was revoked, the mailbox needs reconnecting", account.EmailAddress)

	return fiber.NewError(fiber.StatusFailedDependency, message)
}

func int64OrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
