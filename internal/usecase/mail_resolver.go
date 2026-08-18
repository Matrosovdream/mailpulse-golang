package usecase

import (
	"encoding/json"
	"fmt"

	"mailpulse/internal/entity"
	"mailpulse/internal/gateway/mail"
	"mailpulse/internal/gateway/secret"
	"mailpulse/internal/model"
	"mailpulse/internal/repository"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// MailResolver turns a stored mail_accounts row into the client that serves it
// plus the provider-agnostic view that client expects.
//
// It exists so the account usecase and the sync pipeline share one path:
// look up the provider row for its kind, find the registered client, decrypt
// the credentials. Decryption happens here, in the usecase layer — the gateway
// never sees the cipher.
type MailResolver struct {
	Rows     *repository.MailProviderRepository
	Registry *mail.Registry
	Cipher   *secret.Cipher
}

func NewMailResolver(rows *repository.MailProviderRepository, registry *mail.Registry,
	cipher *secret.Cipher) *MailResolver {
	return &MailResolver{Rows: rows, Registry: registry, Cipher: cipher}
}

func (r *MailResolver) Row(db *gorm.DB, slug string) (*entity.MailProvider, error) {
	row := new(entity.MailProvider)
	if err := r.Rows.FindBySlug(db, row, slug); err != nil {
		return nil, fiber.NewError(fiber.StatusBadRequest,
			fmt.Sprintf("unknown mail provider %q", slug))
	}
	return row, nil
}

func (r *MailResolver) Resolve(db *gorm.DB, account *entity.MailAccount) (mail.Provider, mail.Account, error) {
	row, err := r.Row(db, account.Provider)
	if err != nil {
		return nil, mail.Account{}, err
	}

	client, err := r.Registry.Get(row.Kind)
	if err != nil {
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
