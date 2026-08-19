package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"mailpulse/internal/entity"
	gwnotifier "mailpulse/internal/gateway/notifier"
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

type NotifierUseCase struct {
	DB        *gorm.DB
	Log       *logrus.Logger
	Validate  *validator.Validate
	Notifiers *repository.NotifierRepository
	Channels  *gwnotifier.Registry
	Cipher    *secret.Cipher
	Audit     *AuditUseCase
}

func NewNotifierUseCase(db *gorm.DB, log *logrus.Logger, validate *validator.Validate,
	notifiers *repository.NotifierRepository, channels *gwnotifier.Registry,
	cipher *secret.Cipher, audit *AuditUseCase) *NotifierUseCase {
	return &NotifierUseCase{
		DB: db, Log: log, Validate: validate,
		Notifiers: notifiers, Channels: channels, Cipher: cipher, Audit: audit,
	}
}

func (c *NotifierUseCase) Create(ctx context.Context, request *model.CreateNotifierRequest) (*model.NotifierResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		c.Log.Warnf("Invalid notifier request : %+v", err)
		return nil, fiber.ErrBadRequest
	}

	channel, err := c.Channels.Get(request.Type)
	if err != nil {
		return nil, err
	}

	// the channel owns what a valid config looks like, so a broken one fails
	// here rather than at send time
	if err := channel.Validate(request.Config); err != nil {
		return nil, err
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	total, err := c.Notifiers.CountByName(tx, request.UserID, request.Name, "")
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}
	if total > 0 {
		return nil, fiber.NewError(fiber.StatusConflict, "you already have a notifier called "+request.Name)
	}

	notifier := &entity.Notifier{
		ID:        uuid.NewString(),
		UserID:    request.UserID,
		Type:      request.Type,
		Name:      request.Name,
		Config:    jsonOrDefault(request.Config, "{}"),
		Status:    entity.NotifierStatusPending,
		IsDefault: request.IsDefault,
	}

	if len(request.Secrets) > 0 {
		encrypted, err := c.Cipher.Encrypt(string(request.Secrets))
		if err != nil {
			return nil, fiber.ErrInternalServerError
		}
		notifier.Secrets = &encrypted
	}

	// a channel that cannot prove ownership is usable immediately
	if !channel.RequiresVerification() {
		notifier.Status = entity.NotifierStatusVerified
		notifier.VerifiedAt = ptr(time.Now().UnixMilli())
	} else {
		code := verificationCode()
		notifier.VerificationCode = &code
		notifier.VerificationExpiresAt = ptr(time.Now().Add(30 * time.Minute).UnixMilli())
	}

	if err := c.Notifiers.Create(tx, notifier); err != nil {
		c.Log.Warnf("Failed create notifier : %+v", err)
		return nil, fiber.ErrInternalServerError
	}

	if notifier.IsDefault {
		if err := c.Notifiers.ClearDefault(tx, request.UserID, notifier.ID); err != nil {
			return nil, fiber.ErrInternalServerError
		}
	}

	c.Audit.Record(ctx, tx, AuditEntry{ActorID: &request.UserID, Action: "notifier.created",
		EntityType: "notifiers", EntityID: &notifier.ID})

	if err := tx.Commit().Error; err != nil {
		return nil, fiber.ErrInternalServerError
	}

	response := converter.NotifierToResponse(notifier)
	return response, nil
}

func (c *NotifierUseCase) List(ctx context.Context, request *model.ListNotifierRequest) ([]model.NotifierResponse, *model.PageMetadata, error) {
	request.Normalize()
	if err := c.Validate.Struct(request); err != nil {
		return nil, nil, fiber.ErrBadRequest
	}

	query := c.Notifiers.Search(c.DB.WithContext(ctx), request.UserID, request.Type, request.Status)

	var notifiers []entity.Notifier
	total, err := c.Notifiers.Paginate(query, &notifiers, request.Page, request.Size)
	if err != nil {
		c.Log.Warnf("Failed list notifiers : %+v", err)
		return nil, nil, fiber.ErrInternalServerError
	}

	metadata := model.NewPageMetadata(request.Page, request.Size, total)
	return converter.NotifiersToResponses(notifiers), &metadata, nil
}

func (c *NotifierUseCase) Get(ctx context.Context, request *model.GetNotifierRequest) (*model.NotifierResponse, error) {
	notifier, err := c.find(c.DB.WithContext(ctx), request.ID, request.UserID)
	if err != nil {
		return nil, err
	}
	return converter.NotifierToResponse(notifier), nil
}

func (c *NotifierUseCase) find(db *gorm.DB, id, userID string) (*entity.Notifier, error) {
	notifier := new(entity.Notifier)
	if err := c.Notifiers.FindByIdAndUser(db, notifier, id, userID); err != nil {
		return nil, fiber.NewError(fiber.StatusNotFound, "notifier not found")
	}
	return notifier, nil
}

func (c *NotifierUseCase) Update(ctx context.Context, request *model.UpdateNotifierRequest) (*model.NotifierResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		return nil, fiber.ErrBadRequest
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	notifier, err := c.find(tx, request.ID, request.UserID)
	if err != nil {
		return nil, err
	}

	if request.Name != "" && request.Name != notifier.Name {
		total, err := c.Notifiers.CountByName(tx, request.UserID, request.Name, notifier.ID)
		if err != nil {
			return nil, fiber.ErrInternalServerError
		}
		if total > 0 {
			return nil, fiber.NewError(fiber.StatusConflict, "you already have a notifier called "+request.Name)
		}
		notifier.Name = request.Name
	}

	if len(request.Config) > 0 {
		channel, err := c.Channels.Get(notifier.Type)
		if err != nil {
			return nil, err
		}
		if err := channel.Validate(request.Config); err != nil {
			return nil, err
		}
		notifier.Config = entity.JSON(request.Config)
	}

	if len(request.Secrets) > 0 {
		encrypted, err := c.Cipher.Encrypt(string(request.Secrets))
		if err != nil {
			return nil, fiber.ErrInternalServerError
		}
		notifier.Secrets = &encrypted
	}

	if request.Status != "" {
		notifier.Status = request.Status
	}

	if request.IsDefault != nil {
		notifier.IsDefault = *request.IsDefault
	}

	if err := c.Notifiers.Update(tx, notifier); err != nil {
		return nil, fiber.ErrInternalServerError
	}

	if notifier.IsDefault {
		if err := c.Notifiers.ClearDefault(tx, request.UserID, notifier.ID); err != nil {
			return nil, fiber.ErrInternalServerError
		}
	}

	c.Audit.Record(ctx, tx, AuditEntry{ActorID: &request.UserID, Action: "notifier.updated",
		EntityType: "notifiers", EntityID: &notifier.ID})

	if err := tx.Commit().Error; err != nil {
		return nil, fiber.ErrInternalServerError
	}

	return converter.NotifierToResponse(notifier), nil
}

func (c *NotifierUseCase) Delete(ctx context.Context, request *model.GetNotifierRequest) (bool, error) {
	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	notifier, err := c.find(tx, request.ID, request.UserID)
	if err != nil {
		return false, err
	}

	// the schema restricts this; naming the watchers is more useful than a
	// foreign key error
	watchers, err := c.Notifiers.UsedByEvents(tx, notifier.ID)
	if err != nil {
		return false, fiber.ErrInternalServerError
	}
	if len(watchers) > 0 {
		return false, fiber.NewError(fiber.StatusConflict,
			"this notifier is still used by: "+strings.Join(watchers, ", "))
	}

	if err := c.Notifiers.Delete(tx, notifier); err != nil {
		return false, fiber.ErrInternalServerError
	}

	c.Audit.Record(ctx, tx, AuditEntry{ActorID: &request.UserID, Action: "notifier.deleted",
		EntityType: "notifiers", EntityID: &notifier.ID})

	if err := tx.Commit().Error; err != nil {
		return false, fiber.ErrInternalServerError
	}

	return true, nil
}

// Verify issues a code with no body, and consumes one when a code is supplied.
func (c *NotifierUseCase) Verify(ctx context.Context, request *model.VerifyNotifierRequest) (*model.VerifyNotifierResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		return nil, fiber.ErrBadRequest
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	notifier, err := c.find(tx, request.ID, request.UserID)
	if err != nil {
		return nil, err
	}

	channel, err := c.Channels.Get(notifier.Type)
	if err != nil {
		return nil, err
	}

	if !channel.RequiresVerification() {
		notifier.Status = entity.NotifierStatusVerified
		notifier.VerifiedAt = ptr(time.Now().UnixMilli())
		if err := c.Notifiers.Update(tx, notifier); err != nil {
			return nil, fiber.ErrInternalServerError
		}
		if err := tx.Commit().Error; err != nil {
			return nil, fiber.ErrInternalServerError
		}
		return &model.VerifyNotifierResponse{
			Status:     notifier.Status,
			VerifiedAt: notifier.VerifiedAt,
			Message:    "This channel does not need verification.",
		}, nil
	}

	if request.Code == "" {
		code := verificationCode()
		notifier.VerificationCode = &code
		notifier.VerificationExpiresAt = ptr(time.Now().Add(30 * time.Minute).UnixMilli())
		notifier.Status = entity.NotifierStatusPending

		if err := c.Notifiers.Update(tx, notifier); err != nil {
			return nil, fiber.ErrInternalServerError
		}
		if err := tx.Commit().Error; err != nil {
			return nil, fiber.ErrInternalServerError
		}

		return &model.VerifyNotifierResponse{
			Status:           notifier.Status,
			VerificationCode: &code,
			Message:          "Send this code to the destination to prove you own it, then call this endpoint again with it.",
		}, nil
	}

	if notifier.VerificationCode == nil || *notifier.VerificationCode != strings.TrimSpace(request.Code) {
		return nil, fiber.NewError(fiber.StatusUnprocessableEntity, "that code does not match")
	}
	if notifier.VerificationExpiresAt != nil && *notifier.VerificationExpiresAt < time.Now().UnixMilli() {
		return nil, fiber.NewError(fiber.StatusUnprocessableEntity, "that code has expired, request a new one")
	}

	notifier.Status = entity.NotifierStatusVerified
	notifier.VerifiedAt = ptr(time.Now().UnixMilli())
	notifier.VerificationCode = nil
	notifier.VerificationExpiresAt = nil

	if err := c.Notifiers.Update(tx, notifier); err != nil {
		return nil, fiber.ErrInternalServerError
	}

	c.Audit.Record(ctx, tx, AuditEntry{ActorID: &request.UserID, Action: "notifier.verified",
		EntityType: "notifiers", EntityID: &notifier.ID})

	if err := tx.Commit().Error; err != nil {
		return nil, fiber.ErrInternalServerError
	}

	return &model.VerifyNotifierResponse{
		Status:     notifier.Status,
		VerifiedAt: notifier.VerifiedAt,
		Message:    "Verified.",
	}, nil
}

// Test sends a real message, so the user finds out now rather than at 3am.
func (c *NotifierUseCase) Test(ctx context.Context, request *model.GetNotifierRequest) (*model.TestNotifierResponse, error) {
	db := c.DB.WithContext(ctx)

	notifier, err := c.find(db, request.ID, request.UserID)
	if err != nil {
		return nil, err
	}

	channel, err := c.Channels.Get(notifier.Type)
	if err != nil {
		return nil, err
	}

	secrets, err := c.decryptSecrets(notifier)
	if err != nil {
		return nil, err
	}

	providerID, sendErr := channel.Send(ctx, json.RawMessage(notifier.Config), secrets, gwnotifier.Message{
		Title: "MailPulse test",
		Body:  fmt.Sprintf("This is a test from your %q notifier. If you can read it, delivery works.", notifier.Name),
	})

	if sendErr != nil {
		notifier.LastError = ptr(sendErr.Error())
		_ = c.Notifiers.Update(db, notifier)
		return &model.TestNotifierResponse{Delivered: false, Error: ptr(sendErr.Error())}, nil
	}

	notifier.LastError = nil
	_ = c.Notifiers.Update(db, notifier)

	return &model.TestNotifierResponse{Delivered: true, ProviderMessageID: nilIfEmpty(providerID)}, nil
}

// HandleTelegramUpdate binds a chat to a pending notifier when the user sends
// their verification code to the bot.
func (c *NotifierUseCase) HandleTelegramUpdate(ctx context.Context, request *model.TelegramWebhookRequest) (bool, error) {
	if request.Message == nil || strings.TrimSpace(request.Message.Text) == "" {
		return true, nil
	}

	code := strings.TrimSpace(request.Message.Text)
	chatID := strconv.FormatInt(request.Message.Chat.ID, 10)

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	notifier := new(entity.Notifier)
	if err := c.Notifiers.FindPendingByCode(tx, notifier, code, time.Now().UnixMilli()); err != nil {
		// unknown codes are ignored: the bot receives every message in the chat
		c.Log.Debugf("Telegram update did not match a pending notifier")
		return true, nil
	}

	config, _ := json.Marshal(map[string]string{"chat_id": chatID})
	notifier.Config = entity.JSON(config)
	notifier.Status = entity.NotifierStatusVerified
	notifier.VerifiedAt = ptr(time.Now().UnixMilli())
	notifier.VerificationCode = nil
	notifier.VerificationExpiresAt = nil

	if err := c.Notifiers.Update(tx, notifier); err != nil {
		return false, fiber.ErrInternalServerError
	}

	c.Audit.Record(ctx, tx, AuditEntry{Action: "notifier.verified_via_telegram",
		EntityType: "notifiers", EntityID: &notifier.ID})

	if err := tx.Commit().Error; err != nil {
		return false, fiber.ErrInternalServerError
	}

	return true, nil
}

func (c *NotifierUseCase) decryptSecrets(notifier *entity.Notifier) (json.RawMessage, error) {
	if notifier.Secrets == nil || *notifier.Secrets == "" {
		return nil, nil
	}

	plaintext, err := c.Cipher.Decrypt(*notifier.Secrets)
	if err != nil {
		c.Log.Warnf("Failed decrypt notifier secrets : %+v", err)
		return nil, fiber.ErrInternalServerError
	}

	return json.RawMessage(plaintext), nil
}

func verificationCode() string {
	return fmt.Sprintf("MP-%04d", rand.Intn(10000))
}
