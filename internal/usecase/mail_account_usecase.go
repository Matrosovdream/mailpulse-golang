package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mailpulse/internal/entity"
	"mailpulse/internal/gateway/mail"
	"mailpulse/internal/gateway/secret"
	"mailpulse/internal/model"
	"mailpulse/internal/model/converter"
	"mailpulse/internal/repository"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type MailAccountUseCase struct {
	DB           *gorm.DB
	Log          *logrus.Logger
	Validate     *validator.Validate
	Accounts     *repository.MailAccountRepository
	ProviderRows *repository.MailProviderRepository
	Resolver     *MailResolver
	Watchers     *repository.WatcherRepository
	SyncRuns     *repository.MailSyncRunRepository
	Providers    *mail.Registry
	Cipher       *secret.Cipher
	Audit        *AuditUseCase
	Pipeline     *PipelineUseCase
}

func NewMailAccountUseCase(db *gorm.DB, log *logrus.Logger, validate *validator.Validate,
	accounts *repository.MailAccountRepository, mailProviders *repository.MailProviderRepository,
	watchers *repository.WatcherRepository,
	syncRuns *repository.MailSyncRunRepository, providers *mail.Registry,
	cipher *secret.Cipher, audit *AuditUseCase, pipeline *PipelineUseCase,
	resolver *MailResolver) *MailAccountUseCase {
	return &MailAccountUseCase{
		DB: db, Log: log, Validate: validate, Resolver: resolver,
		Accounts: accounts, ProviderRows: mailProviders, Watchers: watchers, SyncRuns: syncRuns,
		Providers: providers, Cipher: cipher, Audit: audit, Pipeline: pipeline,
	}
}

func (c *MailAccountUseCase) Create(ctx context.Context, request *model.CreateMailAccountRequest) (*model.MailAccountResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		c.Log.Warnf("Invalid mail account request : %+v", err)
		return nil, fiber.ErrBadRequest
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	provider, err := c.provider(tx, request.Provider)
	if err != nil {
		return nil, err
	}

	authMode := request.AuthMode
	if authMode == "" {
		authMode = provider.AuthModeList()[0]
	}
	if !provider.SupportsAuthMode(authMode) {
		return nil, fiber.NewError(fiber.StatusBadRequest,
			fmt.Sprintf("%s does not support %s, it supports %s",
				provider.Label, authMode, provider.AuthModes))
	}

	settings, err := c.mergeSettings(provider, nil, request.Settings)
	if err != nil {
		return nil, err
	}

	email := strings.ToLower(strings.TrimSpace(request.EmailAddress))

	total, err := c.Accounts.CountByEmail(tx, request.UserID, email)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}
	if total > 0 {
		return nil, fiber.NewError(fiber.StatusConflict, "that mailbox is already connected")
	}

	encrypted, err := c.encryptCredentials(model.MailAccountCredentials{
		Username: request.Username,
		Password: request.Password,
	})
	if err != nil {
		return nil, err
	}

	interval := request.PollIntervalSeconds
	if interval == 0 {
		interval = 120
	}

	account := &entity.MailAccount{
		ID:                  uuid.NewString(),
		UserID:              request.UserID,
		Provider:            provider.Slug,
		EmailAddress:        email,
		DisplayName:         nilIfEmpty(request.DisplayName),
		AuthMode:            authMode,
		Credentials:         encrypted,
		Settings:            settings,
		Status:              entity.MailAccountStatusPending,
		SyncState:           entity.JSON("{}"),
		PollIntervalSeconds: interval,
		NextPollAt:          time.Now().UnixMilli(),
	}

	if err := c.Accounts.Create(tx, account); err != nil {
		c.Log.Warnf("Failed create mail account : %+v", err)
		return nil, fiber.ErrInternalServerError
	}

	c.Audit.Record(tx, AuditEntry{ActorID: &request.UserID, Action: "mail_account.created",
		EntityType: "mail_accounts", EntityID: &account.ID})

	if err := tx.Commit().Error; err != nil {
		return nil, fiber.ErrInternalServerError
	}

	return c.describe(c.DB.WithContext(ctx), account), nil
}

// provider resolves the slug against the table and refuses one with no client
// registered, so a seed row can never point at an implementation that is not
// compiled in.
func (c *MailAccountUseCase) provider(db *gorm.DB, slug string) (*entity.MailProvider, error) {
	provider := new(entity.MailProvider)
	if err := c.ProviderRows.FindBySlug(db, provider, slug); err != nil {
		return nil, fiber.NewError(fiber.StatusBadRequest,
			fmt.Sprintf("unknown mail provider %q", slug))
	}

	if !provider.Enabled {
		return nil, fiber.NewError(fiber.StatusBadRequest,
			fmt.Sprintf("%s is not available yet", provider.Label))
	}

	if !c.Providers.Has(provider.Kind) {
		return nil, fiber.NewError(fiber.StatusNotImplemented,
			fmt.Sprintf("%s needs the %s client, which is not built yet", provider.Label, provider.Kind))
	}

	return provider, nil
}

// mergeSettings layers the caller's settings over the provider's presets, so a
// connect form only has to send what the user actually typed.
func (c *MailAccountUseCase) mergeSettings(provider *entity.MailProvider, existing entity.JSON,
	incoming json.RawMessage) (entity.JSON, error) {
	merged := map[string]any{}

	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &merged); err != nil {
			return nil, fiber.ErrInternalServerError
		}
	} else {
		if provider.DefaultHost != nil {
			merged["host"] = *provider.DefaultHost
		}
		if provider.DefaultPort != nil {
			merged["port"] = *provider.DefaultPort
		}
		merged["use_tls"] = provider.DefaultUseTLS
	}

	if len(incoming) > 0 {
		supplied := map[string]any{}
		if err := json.Unmarshal(incoming, &supplied); err != nil {
			return nil, fiber.NewError(fiber.StatusBadRequest, "settings must be a JSON object")
		}
		for key, value := range supplied {
			merged[key] = value
		}
	}

	encoded, err := json.Marshal(merged)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	return entity.JSON(encoded), nil
}

func (c *MailAccountUseCase) encryptCredentials(credentials model.MailAccountCredentials) (string, error) {
	return c.Resolver.Encrypt(credentials)
}

// describe decorates a response with the provider label and kind, which the
// SPA needs to pick the right icon and form.
func (c *MailAccountUseCase) describe(db *gorm.DB, account *entity.MailAccount) *model.MailAccountResponse {
	response := converter.MailAccountToResponse(account)

	provider := new(entity.MailProvider)
	if err := c.ProviderRows.FindBySlug(db, provider, account.Provider); err == nil {
		response.ProviderLabel = provider.Label
		response.Kind = provider.Kind
	}

	return response
}

// decorate fills provider labels for a page of rows in one query.
func (c *MailAccountUseCase) decorate(db *gorm.DB, responses []model.MailAccountResponse) {
	rows, err := c.ProviderRows.FindAll(db)
	if err != nil {
		return
	}

	byslug := make(map[string]entity.MailProvider, len(rows))
	for i := range rows {
		byslug[rows[i].Slug] = rows[i]
	}

	for i := range responses {
		if row, ok := byslug[responses[i].Provider]; ok {
			responses[i].ProviderLabel = row.Label
			responses[i].Kind = row.Kind
		}
	}
}

func (c *MailAccountUseCase) List(ctx context.Context, request *model.ListMailAccountRequest) ([]model.MailAccountResponse, *model.PageMetadata, error) {
	request.Normalize()
	if err := c.Validate.Struct(request); err != nil {
		return nil, nil, fiber.ErrBadRequest
	}

	query := c.Accounts.Search(c.DB.WithContext(ctx), request.UserID, request.Status, request.Provider)

	var accounts []entity.MailAccount
	total, err := c.Accounts.Paginate(query, &accounts, request.Page, request.Size)
	if err != nil {
		c.Log.Warnf("Failed list mail accounts : %+v", err)
		return nil, nil, fiber.ErrInternalServerError
	}

	metadata := model.NewPageMetadata(request.Page, request.Size, total)
	responses := converter.MailAccountsToResponses(accounts)
	c.decorate(c.DB.WithContext(ctx), responses)

	return responses, &metadata, nil
}

func (c *MailAccountUseCase) Get(ctx context.Context, request *model.GetMailAccountRequest) (*model.MailAccountResponse, error) {
	db := c.DB.WithContext(ctx)

	account, err := c.find(db, request.ID, request.UserID)
	if err != nil {
		return nil, err
	}

	return c.describe(db, account), nil
}

// find is the tenant-scoped read every other method starts from.
func (c *MailAccountUseCase) find(db *gorm.DB, id, userID string) (*entity.MailAccount, error) {
	account := new(entity.MailAccount)
	if err := c.Accounts.FindByIdAndUser(db, account, id, userID); err != nil {
		return nil, fiber.NewError(fiber.StatusNotFound, "mail account not found")
	}
	return account, nil
}

func (c *MailAccountUseCase) Update(ctx context.Context, request *model.UpdateMailAccountRequest) (*model.MailAccountResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		return nil, fiber.ErrBadRequest
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	account, err := c.find(tx, request.ID, request.UserID)
	if err != nil {
		return nil, err
	}

	if request.DisplayName != "" {
		account.DisplayName = &request.DisplayName
	}
	if request.PollIntervalSeconds > 0 {
		account.PollIntervalSeconds = request.PollIntervalSeconds
	}
	if request.Status != "" {
		account.Status = request.Status
	}

	// changing where we connect sends the account back for verification
	if len(request.Settings) > 0 {
		provider, err := c.provider(tx, account.Provider)
		if err != nil {
			return nil, err
		}

		merged, err := c.mergeSettings(provider, account.Settings, request.Settings)
		if err != nil {
			return nil, err
		}

		account.Settings = merged
		account.Status = entity.MailAccountStatusPending
	}

	// Credentials are decrypted and merged rather than rebuilt: changing only
	// the password must not wipe the username, which some servers need and
	// which the caller has no way to re-send (it is never returned).
	if request.Password != "" || request.Username != "" {
		credentials, err := c.decryptCredentials(account)
		if err != nil {
			return nil, err
		}

		if request.Username != "" {
			credentials.Username = request.Username
		}
		if request.Password != "" {
			credentials.Password = request.Password
		}

		encrypted, err := c.encryptCredentials(credentials)
		if err != nil {
			return nil, err
		}

		account.Credentials = encrypted
		account.Status = entity.MailAccountStatusPending
	}

	if err := c.Accounts.Update(tx, account); err != nil {
		return nil, fiber.ErrInternalServerError
	}

	c.Audit.Record(tx, AuditEntry{ActorID: &request.UserID, Action: "mail_account.updated",
		EntityType: "mail_accounts", EntityID: &account.ID})

	if err := tx.Commit().Error; err != nil {
		return nil, fiber.ErrInternalServerError
	}

	return c.describe(c.DB.WithContext(ctx), account), nil
}

func (c *MailAccountUseCase) Delete(ctx context.Context, request *model.GetMailAccountRequest) (bool, error) {
	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	account, err := c.find(tx, request.ID, request.UserID)
	if err != nil {
		return false, err
	}

	// the schema restricts this delete; catching it here lets the API name the
	// watchers that are in the way instead of surfacing a constraint violation
	names, err := c.Watchers.NamesByMailAccount(tx, account.ID)
	if err != nil {
		return false, fiber.ErrInternalServerError
	}
	if len(names) > 0 {
		return false, fiber.NewError(fiber.StatusConflict,
			"this mailbox still has watchers: "+strings.Join(names, ", "))
	}

	if err := c.Accounts.Delete(tx, account); err != nil {
		return false, fiber.ErrInternalServerError
	}

	c.Audit.Record(tx, AuditEntry{ActorID: &request.UserID, Action: "mail_account.deleted",
		EntityType: "mail_accounts", EntityID: &account.ID})

	if err := tx.Commit().Error; err != nil {
		return false, fiber.ErrInternalServerError
	}

	return true, nil
}

// decryptCredentials reads the encrypted blob back into its parts.
func (c *MailAccountUseCase) decryptCredentials(account *entity.MailAccount) (model.MailAccountCredentials, error) {
	return c.Resolver.Credentials(account)
}

func (c *MailAccountUseCase) resolve(db *gorm.DB, account *entity.MailAccount) (mail.Provider, mail.Account, error) {
	return c.Resolver.Resolve(db, account)
}

func (c *MailAccountUseCase) Verify(ctx context.Context, request *model.GetMailAccountRequest) (*model.VerifyMailAccountResponse, error) {
	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	account, err := c.find(tx, request.ID, request.UserID)
	if err != nil {
		return nil, err
	}

	client, target, err := c.resolve(tx, account)
	if err != nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	folders, verifyErr := client.Verify(ctx, target)

	if verifyErr != nil {
		account.Status = entity.MailAccountStatusError
		account.LastError = ptr(verifyErr.Error())
		_ = c.Accounts.Update(tx, account)
		_ = tx.Commit().Error
		return nil, fiber.NewError(fiber.StatusUnprocessableEntity, verifyErr.Error())
	}

	account.Status = entity.MailAccountStatusVerified
	account.LastVerifiedAt = &now
	account.LastError = nil

	if err := c.Accounts.Update(tx, account); err != nil {
		return nil, fiber.ErrInternalServerError
	}

	c.Audit.Record(tx, AuditEntry{ActorID: &request.UserID, Action: "mail_account.verified",
		EntityType: "mail_accounts", EntityID: &account.ID})

	if err := tx.Commit().Error; err != nil {
		return nil, fiber.ErrInternalServerError
	}

	return &model.VerifyMailAccountResponse{
		Status:         account.Status,
		FoldersFound:   len(folders),
		LastVerifiedAt: now,
	}, nil
}

func (c *MailAccountUseCase) Folders(ctx context.Context, request *model.GetMailAccountRequest) ([]model.FolderResponse, error) {
	db := c.DB.WithContext(ctx)

	account, err := c.find(db, request.ID, request.UserID)
	if err != nil {
		return nil, err
	}

	client, target, err := c.resolve(db, account)
	if err != nil {
		return nil, err
	}

	folders, err := mail.ListFolders(ctx, client, target)
	if err != nil {
		return nil, err
	}

	responses := make([]model.FolderResponse, 0, len(folders))
	for _, folder := range folders {
		responses = append(responses, model.FolderResponse{Name: folder.Name, MessageCount: folder.MessageCount})
	}

	return responses, nil
}

// Sync runs a fetch now instead of waiting for next_poll_at.
func (c *MailAccountUseCase) Sync(ctx context.Context, request *model.GetMailAccountRequest) (*model.SyncMailAccountResponse, error) {
	account, err := c.find(c.DB.WithContext(ctx), request.ID, request.UserID)
	if err != nil {
		return nil, err
	}

	result, err := c.Pipeline.SyncAccount(ctx, account)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (c *MailAccountUseCase) ListSyncRuns(ctx context.Context, request *model.GetMailAccountRequest, page *model.PageRequest) ([]model.MailSyncRunResponse, *model.PageMetadata, error) {
	page.Normalize()

	account, err := c.find(c.DB.WithContext(ctx), request.ID, request.UserID)
	if err != nil {
		return nil, nil, err
	}

	query := c.SyncRuns.Search(c.DB.WithContext(ctx), account.ID, "")

	var runs []entity.MailSyncRun
	total, err := c.SyncRuns.Paginate(query, &runs, page.Page, page.Size)
	if err != nil {
		return nil, nil, fiber.ErrInternalServerError
	}

	metadata := model.NewPageMetadata(page.Page, page.Size, total)
	return converter.SyncRunsToResponses(runs), &metadata, nil
}

// ReverifyDue re-checks stored credentials on a schedule.
//
// Credentials rot: app passwords get revoked, hosts change, tokens expire. This
// is what turns "your watcher silently stopped weeks ago" into a status the
// dashboard can show. A failing account is marked error, which also takes it
// out of the poll queue, and a recovering one is picked back up automatically.
func (c *MailAccountUseCase) ReverifyDue(ctx context.Context, olderThan time.Duration, limit int) (checked int, failed int, err error) {
	db := c.DB.WithContext(ctx)
	before := time.Now().Add(-olderThan).UnixMilli()

	var accounts []entity.MailAccount
	err = db.Transaction(func(tx *gorm.DB) error {
		found, txErr := c.Accounts.FindStale(tx, before, limit)
		if txErr != nil {
			return txErr
		}
		accounts = found

		// stamp the attempt inside the claim so a second worker starting now
		// does not re-check the same accounts
		now := time.Now().UnixMilli()
		for i := range accounts {
			if txErr := tx.Model(&entity.MailAccount{}).Where("id = ?", accounts[i].ID).
				Update("last_verified_at", now).Error; txErr != nil {
				return txErr
			}
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}

	for i := range accounts {
		account := &accounts[i]
		checked++

		if c.reverifyOne(ctx, db, account) != nil {
			failed++
		}
	}

	return checked, failed, nil
}

func (c *MailAccountUseCase) reverifyOne(ctx context.Context, db *gorm.DB, account *entity.MailAccount) error {
	fail := func(cause error) error {
		account.Status = entity.MailAccountStatusError
		account.LastError = ptr(cause.Error())
		if err := c.Accounts.Update(db, account); err != nil {
			c.Log.WithError(err).Warnf("Failed to record a credential failure for %s", account.ID)
		}
		c.Log.Warnf("Credentials for %s no longer work: %v", account.EmailAddress, cause)
		return cause
	}

	client, target, err := c.resolve(db, account)
	if err != nil {
		return fail(err)
	}

	if _, err := client.Verify(ctx, target); err != nil {
		return fail(err)
	}

	wasBroken := account.Status != entity.MailAccountStatusVerified

	account.Status = entity.MailAccountStatusVerified
	account.LastVerifiedAt = ptr(time.Now().UnixMilli())
	account.LastError = nil

	if err := c.Accounts.Update(db, account); err != nil {
		c.Log.WithError(err).Warnf("Failed to record a credential success for %s", account.ID)
		return err
	}

	if wasBroken {
		c.Log.Infof("Credentials for %s work again, resuming polling", account.EmailAddress)
	}

	return nil
}

// Authorize returns the consent URL the SPA redirects to. Wiring a real client
// id and exchanging the code belongs in a provider-specific gateway; the route
// and the state handshake are here so the flow can be built against it.
func (c *MailAccountUseCase) Authorize(ctx context.Context, request *model.OAuthAuthorizeRequest) (*model.OAuthAuthorizeResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		return nil, fiber.ErrBadRequest
	}

	return nil, fiber.NewError(fiber.StatusNotImplemented,
		"OAuth for "+request.Provider+" is not configured yet: connect the mailbox over IMAP, or set the provider client id and secret")
}

func (c *MailAccountUseCase) Callback(ctx context.Context, request *model.OAuthCallbackRequest) (*model.MailAccountResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		return nil, fiber.ErrBadRequest
	}

	return nil, fiber.NewError(fiber.StatusNotImplemented,
		"OAuth for "+request.Provider+" is not configured yet")
}
