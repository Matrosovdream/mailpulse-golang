package http

import (
	"strconv"
	"time"

	"mailpulse/internal/model"
	"mailpulse/internal/usecase"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

// CatalogController exposes the registries so the SPA can render its forms
// from the server's capabilities rather than a hard-coded copy of them.
type CatalogController struct {
	Log     *logrus.Logger
	UseCase *usecase.CatalogUseCase

	// StaticMaxAge is how long a caller may hold the three registry-backed
	// catalogs. They are compiled-in tables that only change on deploy, and the
	// SPA asks for all three while rendering its forms, so this removes
	// requests at the edge rather than saving work on the server — the
	// handlers themselves already cost nothing. Zero sends no header at all.
	StaticMaxAge time.Duration
}

func NewCatalogController(useCase *usecase.CatalogUseCase, log *logrus.Logger,
	staticMaxAge time.Duration) *CatalogController {
	return &CatalogController{Log: log, UseCase: useCase, StaticMaxAge: staticMaxAge}
}

// cacheStatic marks a response as holdable for StaticMaxAge.
//
// private, not public. These three routes sit behind the auth middleware, and a
// shared cache that keyed a response served against one bearer token would hand
// it to the next caller. Nothing user-specific is in these payloads today, and
// this header should not be the only thing standing between that and a leak if
// one is ever added.
func (c *CatalogController) cacheStatic(ctx *fiber.Ctx) {
	if c.StaticMaxAge <= 0 {
		return
	}

	ctx.Set(fiber.HeaderCacheControl,
		"private, max-age="+strconv.Itoa(int(c.StaticMaxAge.Seconds())))
}

// MailProviderTypes drives the connect form: which mailboxes a user may add,
// the host/port preset for each, and what that client can do.
func (c *CatalogController) MailProviderTypes(ctx *fiber.Ctx) error {
	response, err := c.UseCase.MailProviderTypes(ctx.UserContext())
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[[]model.MailProviderResponse]{Data: response})
}

func (c *CatalogController) EventTypes(ctx *fiber.Ctx) error {
	c.cacheStatic(ctx)

	return ctx.JSON(model.WebResponse[[]model.EventTypeResponse]{Data: c.UseCase.EventTypes()})
}

func (c *CatalogController) NotifierTypes(ctx *fiber.Ctx) error {
	c.cacheStatic(ctx)

	return ctx.JSON(model.WebResponse[[]model.NotifierTypeResponse]{Data: c.UseCase.NotifierTypes()})
}

func (c *CatalogController) FilterFields(ctx *fiber.Ctx) error {
	c.cacheStatic(ctx)

	return ctx.JSON(model.WebResponse[[]model.FilterFieldResponse]{Data: c.UseCase.FilterFields()})
}

// Health is public so a load balancer can call it, and reports each dependency
// separately so the answer says which one is down.
func (c *CatalogController) Health(ctx *fiber.Ctx) error {
	response := c.UseCase.Health(ctx.UserContext())

	status := fiber.StatusOK
	if response.Database != "ok" || response.Redis != "ok" {
		status = fiber.StatusServiceUnavailable
	}

	return ctx.Status(status).JSON(model.WebResponse[*model.HealthResponse]{Data: response})
}
