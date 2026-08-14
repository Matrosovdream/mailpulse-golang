package http

import (
	"mailpulse/internal/delivery/http/middleware"
	"mailpulse/internal/model"
	"mailpulse/internal/usecase"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

type WatcherEventController struct {
	Log     *logrus.Logger
	UseCase *usecase.WatcherEventUseCase
}

func NewWatcherEventController(useCase *usecase.WatcherEventUseCase, log *logrus.Logger) *WatcherEventController {
	return &WatcherEventController{Log: log, UseCase: useCase}
}

func (c *WatcherEventController) List(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.List(ctx.UserContext(), &model.ListWatcherEventRequest{
		UserID: auth.ID, WatcherID: ctx.Params("watcherId"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[[]model.WatcherEventResponse]{Data: response})
}

func (c *WatcherEventController) Create(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	request := new(model.CreateWatcherEventRequest)
	if err := ctx.BodyParser(request); err != nil {
		return fiber.ErrBadRequest
	}
	request.UserID = auth.ID
	request.WatcherID = ctx.Params("watcherId")

	response, err := c.UseCase.Create(ctx.UserContext(), request)
	if err != nil {
		return err
	}

	return ctx.Status(fiber.StatusCreated).JSON(model.WebResponse[*model.WatcherEventResponse]{Data: response})
}

func (c *WatcherEventController) Get(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.Get(ctx.UserContext(), &model.GetWatcherEventRequest{
		UserID: auth.ID, WatcherID: ctx.Params("watcherId"), ID: ctx.Params("eventId"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.WatcherEventResponse]{Data: response})
}

func (c *WatcherEventController) Update(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	request := new(model.UpdateWatcherEventRequest)
	if err := ctx.BodyParser(request); err != nil {
		return fiber.ErrBadRequest
	}
	request.UserID = auth.ID
	request.WatcherID = ctx.Params("watcherId")
	request.ID = ctx.Params("eventId")

	response, err := c.UseCase.Update(ctx.UserContext(), request)
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.WatcherEventResponse]{Data: response})
}

func (c *WatcherEventController) Delete(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.Delete(ctx.UserContext(), &model.GetWatcherEventRequest{
		UserID: auth.ID, WatcherID: ctx.Params("watcherId"), ID: ctx.Params("eventId"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[bool]{Data: response})
}

func (c *WatcherEventController) Reorder(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	request := new(model.ReorderWatcherEventRequest)
	if err := ctx.BodyParser(request); err != nil {
		return fiber.ErrBadRequest
	}
	request.UserID = auth.ID
	request.WatcherID = ctx.Params("watcherId")

	response, err := c.UseCase.Reorder(ctx.UserContext(), request)
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[[]model.WatcherEventResponse]{Data: response})
}

func (c *WatcherEventController) Test(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	request := new(model.TestWatcherEventRequest)
	_ = ctx.BodyParser(request)
	request.UserID = auth.ID
	request.WatcherID = ctx.Params("watcherId")
	request.ID = ctx.Params("eventId")

	response, err := c.UseCase.Test(ctx.UserContext(), request)
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.EventRunResponse]{Data: response})
}
