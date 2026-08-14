package http

import (
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
}

func NewCatalogController(useCase *usecase.CatalogUseCase, log *logrus.Logger) *CatalogController {
	return &CatalogController{Log: log, UseCase: useCase}
}

func (c *CatalogController) EventTypes(ctx *fiber.Ctx) error {
	return ctx.JSON(model.WebResponse[[]model.EventTypeResponse]{Data: c.UseCase.EventTypes()})
}

func (c *CatalogController) NotifierTypes(ctx *fiber.Ctx) error {
	return ctx.JSON(model.WebResponse[[]model.NotifierTypeResponse]{Data: c.UseCase.NotifierTypes()})
}

func (c *CatalogController) FilterFields(ctx *fiber.Ctx) error {
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
