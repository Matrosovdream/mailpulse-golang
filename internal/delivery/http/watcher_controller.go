package http

import (
	"mailpulse/internal/delivery/http/middleware"
	"mailpulse/internal/entity"
	"mailpulse/internal/gateway/mail"
	"mailpulse/internal/model"
	"mailpulse/internal/usecase"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

type WatcherController struct {
	Log       *logrus.Logger
	UseCase   *usecase.WatcherUseCase
	Providers *mail.Registry
	Accounts  *usecase.MailAccountUseCase
}

func NewWatcherController(useCase *usecase.WatcherUseCase, providers *mail.Registry,
	accounts *usecase.MailAccountUseCase, log *logrus.Logger) *WatcherController {
	return &WatcherController{Log: log, UseCase: useCase, Providers: providers, Accounts: accounts}
}

func (c *WatcherController) List(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, metadata, err := c.UseCase.List(ctx.UserContext(), &model.ListWatcherRequest{
		PageRequest:   paging(ctx),
		UserID:        auth.ID,
		Status:        ctx.Query("status"),
		MailAccountID: ctx.Query("mail_account_id"),
		Query:         ctx.Query("q"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[[]model.WatcherResponse]{Data: response, Paging: metadata})
}

func (c *WatcherController) Create(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	request := new(model.CreateWatcherRequest)
	if err := ctx.BodyParser(request); err != nil {
		return fiber.ErrBadRequest
	}
	request.UserID = auth.ID

	response, err := c.UseCase.Create(ctx.UserContext(), request)
	if err != nil {
		return err
	}

	return ctx.Status(fiber.StatusCreated).JSON(model.WebResponse[*model.WatcherResponse]{Data: response})
}

func (c *WatcherController) Get(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.Get(ctx.UserContext(), &model.GetWatcherRequest{
		UserID: auth.ID, ID: ctx.Params("watcherId"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.WatcherResponse]{Data: response})
}

func (c *WatcherController) Update(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	request := new(model.UpdateWatcherRequest)
	if err := ctx.BodyParser(request); err != nil {
		return fiber.ErrBadRequest
	}
	request.UserID = auth.ID
	request.ID = ctx.Params("watcherId")

	response, err := c.UseCase.Update(ctx.UserContext(), request)
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.WatcherResponse]{Data: response})
}

func (c *WatcherController) Delete(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.Delete(ctx.UserContext(), &model.GetWatcherRequest{
		UserID: auth.ID, ID: ctx.Params("watcherId"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[bool]{Data: response})
}

// setStatus backs the four status actions, which differ only in target state.
func (c *WatcherController) setStatus(ctx *fiber.Ctx, status string) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.SetStatus(ctx.UserContext(), &model.SetWatcherStatusRequest{
		UserID: auth.ID, ID: ctx.Params("watcherId"), Status: status,
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.WatcherResponse]{Data: response})
}

func (c *WatcherController) Archive(ctx *fiber.Ctx) error {
	return c.setStatus(ctx, entity.WatcherStatusArchived)
}

// Restore brings an archived watcher back paused rather than active, so it
// cannot start firing before its owner has looked at it.
func (c *WatcherController) Restore(ctx *fiber.Ctx) error {
	return c.setStatus(ctx, entity.WatcherStatusPaused)
}

func (c *WatcherController) Pause(ctx *fiber.Ctx) error {
	return c.setStatus(ctx, entity.WatcherStatusPaused)
}

func (c *WatcherController) Resume(ctx *fiber.Ctx) error {
	return c.setStatus(ctx, entity.WatcherStatusActive)
}

func (c *WatcherController) GetFilters(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.GetFilters(ctx.UserContext(), &model.GetWatcherRequest{
		UserID: auth.ID, ID: ctx.Params("watcherId"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[[]model.WatcherFilterResponse]{Data: response})
}

func (c *WatcherController) ReplaceFilters(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	request := new(model.ReplaceFiltersRequest)
	if err := ctx.BodyParser(request); err != nil {
		return fiber.ErrBadRequest
	}
	request.UserID = auth.ID
	request.WatcherID = ctx.Params("watcherId")

	response, err := c.UseCase.ReplaceFilters(ctx.UserContext(), request)
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[[]model.WatcherFilterResponse]{Data: response})
}

func (c *WatcherController) Test(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	request := new(model.TestWatcherRequest)
	_ = ctx.BodyParser(request)
	request.UserID = auth.ID
	request.ID = ctx.Params("watcherId")

	response, err := c.UseCase.Test(ctx.UserContext(), request, c.Providers, c.Accounts)
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.TestWatcherResponse]{Data: response})
}

func (c *WatcherController) Stats(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.Stats(ctx.UserContext(), &model.WatcherStatsRequest{
		UserID: auth.ID,
		ID:     ctx.Params("watcherId"),
		Days:   ctx.QueryInt("days", 30),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.WatcherStatsResponse]{Data: response})
}
