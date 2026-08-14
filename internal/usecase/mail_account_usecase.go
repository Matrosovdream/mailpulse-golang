package usecase

import (
	"context"
	"encoding/json"
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
	DB        *gorm.DB
	Log       *logrus.Logger
	Validate  *validator.Validate
	Accounts  *repository.MailAccountRepository
	Watchers  *repository.WatcherRepository
	SyncRuns  *repository.MailSyncRunRepository
	Providers *mail.Registry
	Cipher    *secret.Cipher
	Audit     *AuditUseCase
	Pipeline  *PipelineUseCase
}

func NewMailAccountUseCase(db *gorm.DB, log *logrus.Logger, validate *validator.Validate,
	accounts *repository.MailAccountRepository, watchers *repository.WatcherRepository,
	syncRuns *repository.MailSyncRunRepository, providers *mail.Registry,
	cipher *secret.Cipher, audit *AuditUseCase, pipeline *PipelineUseCase) *MailAccountUseCase {
	return &MailAccountUseCase{
		DB: db, Log: log, Validate: validate,
		Accounts: accounts, Watchers: watchers, SyncRuns: syncRuns,
		Providers: providers, Cipher: cipher, Audit: audit, Pipeline: pipeline,
	}
}

func (c *MailAccountUseCase) Create(ctx context.Context, request *model.CreateMailAccountRequest) (*model.MailAccountResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		c.Log.Warnf("Invalid mail account request : %+v", err)
		return nil, fiber.ErrBadRequest
	}

	if request.Provider == entity.MailProviderIMAP {
		if request.ImapHost == "" || request.ImapPort == 0 {
			return nil, fiber.NewError(fiber.StatusBadRequest, "imap accounts need imap_host and imap_port")
		}
		if request.AuthType == entity.MailAuthPassword && request.Password == "" {
			return nil, fiber.NewError(fiber.StatusBadRequest, "imap accounts need a password")
		}
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	email := strings.ToLower(strings.TrimSpace(request.EmailAddress))

	total, err := c.Accounts.CountByEmail(tx, request.UserID, email)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}
	if total > 0 {
		return nil, fiber.NewError(fiber.StatusConflict, "that mailbox is already connected")
	}

	credentials, err := model.MailAccountCredentials{Password: request.Password}.Encode()
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	encrypted, err := c.Cipher.Encrypt(credentials)
	if err != nil {
		c.Log.Warnf("Failed encrypt credentials : %+v", err)
		return nil, fiber.ErrInternalServerError
	}

	interval := request.PollIntervalSeconds
	if interval == 0 {
		interval = 120
	}

	useTLS := true
	if request.ImapUseTLS != nil {
		useTLS = *request.ImapUseTLS
	}

	account := &entity.MailAccount{
		ID:                  uuid.NewString(),
		UserID:              request.UserID,
		Provider:            request.Provider,
		EmailAddress:        email,
		DisplayName:         nilIfEmpty(request.DisplayName),
		AuthType:            request.AuthType,
		Credentials:         encrypted,
		ImapHost:            nilIfEmpty(request.ImapHost),
		ImapUseTLS:          useTLS,
		Status:              entity.MailAccountStatusPending,
		SyncState:           entity.JSON("{}"),
		PollIntervalSeconds: interval,
		NextPollAt:          time.Now().UnixMilli(),
	}
	if request.ImapPort > 0 {
		account.ImapPort = &request.ImapPort
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

	return converter.MailAccountToResponse(account), nil
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
	return converter.MailAccountsToResponses(accounts), &metadata, nil
}

func (c *MailAccountUseCase) Get(ctx context.Context, request *model.GetMailAccountRequest) (*model.MailAccountResponse, error) {
	account, err := c.find(c.DB.WithContext(ctx), request.ID, request.UserID)
	if err != nil {
		return nil, err
	}
	return converter.MailAccountToResponse(account), nil
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
	if request.ImapHost != "" {
		account.ImapHost = &request.ImapHost
	}
	if request.ImapPort > 0 {
		account.ImapPort = &request.ImapPort
	}
	if request.PollIntervalSeconds > 0 {
		account.PollIntervalSeconds = request.PollIntervalSeconds
	}
	if request.Status != "" {
		account.Status = request.Status
	}

	// re-entering the password sends the account back for verification
	if request.Password != "" {
		credentials, err := model.MailAccountCredentials{Password: request.Password}.Encode()
		if err != nil {
			return nil, fiber.ErrInternalServerError
		}
		encrypted, err := c.Cipher.Encrypt(credentials)
		if err != nil {
			return nil, fiber.ErrInternalServerError
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

	return converter.MailAccountToResponse(account), nil
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

// providerAccount decrypts the stored credentials into the gateway's view.
func (c *MailAccountUseCase) providerAccount(account *entity.MailAccount) (mail.Account, error) {
	plaintext, err := c.Cipher.Decrypt(account.Credentials)
	if err != nil {
		return mail.Account{}, fiber.NewError(fiber.StatusInternalServerError, "stored credentials could not be decrypted")
	}

	var credentials model.MailAccountCredentials
	if plaintext != "" {
		_ = json.Unmarshal([]byte(plaintext), &credentials)
	}

	out := mail.Account{
		ID:           account.ID,
		Provider:     account.Provider,
		EmailAddress: account.EmailAddress,
		AuthType:     account.AuthType,
		UseTLS:       account.ImapUseTLS,
		Password:     credentials.Password,
		AccessToken:  credentials.AccessToken,
		RefreshToken: credentials.RefreshToken,
	}
	if account.ImapHost != nil {
		out.Host = *account.ImapHost
	}
	if account.ImapPort != nil {
		out.Port = *account.ImapPort
	}

	return out, nil
}

func (c *MailAccountUseCase) Verify(ctx context.Context, request *model.GetMailAccountRequest) (*model.VerifyMailAccountResponse, error) {
	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	account, err := c.find(tx, request.ID, request.UserID)
	if err != nil {
		return nil, err
	}

	provider, err := c.Providers.Get(account.Provider)
	if err != nil {
		return nil, err
	}

	target, err := c.providerAccount(account)
	if err != nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	folders, verifyErr := provider.Verify(ctx, target)

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
	account, err := c.find(c.DB.WithContext(ctx), request.ID, request.UserID)
	if err != nil {
		return nil, err
	}

	provider, err := c.Providers.Get(account.Provider)
	if err != nil {
		return nil, err
	}

	target, err := c.providerAccount(account)
	if err != nil {
		return nil, err
	}

	folders, err := provider.Folders(ctx, target)
	if err != nil {
		return nil, fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
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
