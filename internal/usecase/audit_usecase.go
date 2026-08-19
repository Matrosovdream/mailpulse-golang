package usecase

import (
	"context"
	"encoding/json"

	"mailpulse/internal/entity"
	"mailpulse/internal/model"
	"mailpulse/internal/model/converter"
	"mailpulse/internal/repository"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// AuditEntry is what callers describe; the usecase fills in the id and clock.
type AuditEntry struct {
	ActorID            *string
	ImpersonatedUserID *string
	Action             string
	EntityType         string
	EntityID           *string
	Metadata           any
	IP                 *string
	UserAgent          *string
}

type AuditUseCase struct {
	Log      *logrus.Logger
	Validate *validator.Validate
	DB       *gorm.DB
	Logs     *repository.AuditLogRepository
}

func NewAuditUseCase(db *gorm.DB, log *logrus.Logger, validate *validator.Validate,
	logs *repository.AuditLogRepository) *AuditUseCase {
	return &AuditUseCase{DB: db, Log: log, Validate: validate, Logs: logs}
}

// Record writes inside the caller's transaction so the audit row lives or dies
// with the change it describes. A failure is logged, never returned: losing an
// audit line must not roll back the user's actual work.
//
// ctx is here for attribution, not cancellation: it carries the Auth that the
// middleware resolved, which is the only place that knows an impersonated
// session from an ordinary one.
func (c *AuditUseCase) Record(ctx context.Context, tx *gorm.DB, entry AuditEntry) {
	c.attribute(ctx, &entry)

	metadata := entity.JSON("{}")
	if entry.Metadata != nil {
		if raw, err := json.Marshal(entry.Metadata); err == nil {
			metadata = entity.JSON(raw)
		}
	}

	record := &entity.AuditLog{
		ID:                 uuid.NewString(),
		ActorUserID:        entry.ActorID,
		ImpersonatedUserID: entry.ImpersonatedUserID,
		Action:             entry.Action,
		EntityType:         entry.EntityType,
		EntityID:           entry.EntityID,
		Metadata:           metadata,
		IP:                 entry.IP,
		UserAgent:          entry.UserAgent,
	}

	if err := c.Logs.Create(tx, record); err != nil {
		c.Log.WithError(err).Warnf("Failed to write audit log for %s", entry.Action)
	}
}

// attribute rewrites an entry recorded during impersonation so the row names
// the admin who is really acting, with the target in impersonated_user_id.
//
// This lives in one place on purpose. The alternative — passing the pair in at
// each of the two dozen Record calls — is the arrangement that produced the
// original bug, where a session issued by _impersonate wrote audit rows
// indistinguishable from the target user's own.
//
// An entry that already sets ImpersonatedUserID is left alone: that is the
// _impersonate event itself, which is recorded by the admin's own session and
// already names both parties.
func (c *AuditUseCase) attribute(ctx context.Context, entry *AuditEntry) {
	if entry.ImpersonatedUserID != nil {
		return
	}

	auth := model.AuthFromContext(ctx)
	if auth == nil || !auth.Impersonated || auth.ImpersonatedBy == "" {
		return
	}

	actor := auth.ImpersonatedBy
	target := auth.ID
	entry.ActorID = &actor
	entry.ImpersonatedUserID = &target
}

func (c *AuditUseCase) List(ctx context.Context, request *model.ListAuditLogRequest) ([]model.AuditLogResponse, *model.PageMetadata, error) {
	request.Normalize()
	if err := c.Validate.Struct(request); err != nil {
		c.Log.Warnf("Invalid audit log request : %+v", err)
		return nil, nil, fiber.ErrBadRequest
	}

	db := c.DB.WithContext(ctx)
	query := c.Logs.Search(db, request.ActorUserID, request.Action, request.EntityType, request.From, request.To)

	var logs []entity.AuditLog
	total, err := c.Logs.Paginate(query, &logs, request.Page, request.Size)
	if err != nil {
		c.Log.Warnf("Failed list audit logs : %+v", err)
		return nil, nil, fiber.ErrInternalServerError
	}

	emails, err := c.Logs.EmailsFor(db, logs)
	if err != nil {
		c.Log.Warnf("Failed resolve actor emails : %+v", err)
		emails = map[string]string{}
	}

	responses := make([]model.AuditLogResponse, 0, len(logs))
	for i := range logs {
		email := ""
		if logs[i].ActorUserID != nil {
			email = emails[*logs[i].ActorUserID]
		}
		responses = append(responses, *converter.AuditLogToResponse(&logs[i], email))
	}

	metadata := model.NewPageMetadata(request.Page, request.Size, total)
	return responses, &metadata, nil
}
