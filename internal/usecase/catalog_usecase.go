package usecase

import (
	"context"

	"mailpulse/internal/entity"
	gwnotifier "mailpulse/internal/gateway/notifier"
	"mailpulse/internal/model"
	"mailpulse/internal/usecase/event"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// CatalogUseCase publishes what the system can do so the SPA renders its forms
// from the registries instead of hard-coding lists that drift from the server.
type CatalogUseCase struct {
	Log      *logrus.Logger
	DB       *gorm.DB
	Redis    *redis.Client
	Handlers *event.Registry
	Channels *gwnotifier.Registry
	Version  string
}

func NewCatalogUseCase(db *gorm.DB, redisClient *redis.Client, log *logrus.Logger,
	handlers *event.Registry, channels *gwnotifier.Registry, version string) *CatalogUseCase {
	return &CatalogUseCase{
		DB: db, Redis: redisClient, Log: log,
		Handlers: handlers, Channels: channels, Version: version,
	}
}

func (c *CatalogUseCase) EventTypes() []model.EventTypeResponse {
	return c.Handlers.Types()
}

func (c *CatalogUseCase) NotifierTypes() []model.NotifierTypeResponse {
	return c.Channels.Types()
}

// FilterFields mirrors the ck_watcher_filters_field and _operator constraints.
// They are declared together here so a field the database would reject is
// never offered in the builder.
func (c *CatalogUseCase) FilterFields() []model.FilterFieldResponse {
	text := []string{entity.OpContains, entity.OpNotContains, entity.OpEquals,
		entity.OpStartsWith, entity.OpEndsWith, entity.OpRegex}

	return []model.FilterFieldResponse{
		{Field: entity.FieldSubject, Label: "Subject", ValueType: "string", Operators: text},
		{Field: entity.FieldFrom, Label: "From", ValueType: "string", Operators: text,
			Help: "Matches the sender address or the display name."},
		{Field: entity.FieldTo, Label: "To", ValueType: "string", Operators: text},
		{Field: entity.FieldCc, Label: "Cc", ValueType: "string", Operators: text},
		{Field: entity.FieldBody, Label: "Body", ValueType: "string", Operators: text},
		{Field: entity.FieldHeader, Label: "Header", ValueType: "string",
			Operators: append(append([]string{}, text...), entity.OpExists),
			Help:      "Needs a header_name, for example List-Id."},
		{Field: entity.FieldAttachmentName, Label: "Attachment name", ValueType: "string", Operators: text},
		{Field: entity.FieldHasAttachment, Label: "Has attachment", ValueType: "boolean",
			Operators: []string{entity.OpExists}},
		{Field: entity.FieldSize, Label: "Size in bytes", ValueType: "int",
			Operators: []string{entity.OpGt, entity.OpLt, entity.OpEquals}},
	}
}

// Health reports each dependency separately so a probe says which one is down.
func (c *CatalogUseCase) Health(ctx context.Context) *model.HealthResponse {
	response := &model.HealthResponse{Database: "ok", Redis: "ok", Kafka: "not checked", Version: c.Version}

	sqlDB, err := c.DB.DB()
	if err != nil || sqlDB.PingContext(ctx) != nil {
		response.Database = "error"
	}

	if err := c.Redis.Ping(ctx).Err(); err != nil {
		response.Redis = "error"
	}

	return response
}
