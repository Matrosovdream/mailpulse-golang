package http

import (
	"mailpulse/internal/delivery/http/middleware"
	"mailpulse/internal/model"
	"mailpulse/internal/usecase"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

type NotifierController struct {
	Log     *logrus.Logger
	UseCase *usecase.NotifierUseCase
}

func NewNotifierController(useCase *usecase.NotifierUseCase, log *logrus.Logger) *NotifierController {
	return &NotifierController{Log: log, UseCase: useCase}
}

func (c *NotifierController) List(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, metadata, err := c.UseCase.List(ctx.UserContext(), &model.ListNotifierRequest{
		PageRequest: paging(ctx),
		UserID:      auth.ID,
		Type:        ctx.Query("type"),
		Status:      ctx.Query("status"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[[]model.NotifierResponse]{Data: response, Paging: metadata})
}

func (c *NotifierController) Create(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	request := new(model.CreateNotifierRequest)
	if err := ctx.BodyParser(request); err != nil {
		return fiber.ErrBadRequest
	}
	request.UserID = auth.ID

	response, err := c.UseCase.Create(ctx.UserContext(), request)
	if err != nil {
		return err
	}

	return ctx.Status(fiber.StatusCreated).JSON(model.WebResponse[*model.NotifierResponse]{Data: response})
}

func (c *NotifierController) Get(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.Get(ctx.UserContext(), &model.GetNotifierRequest{
		UserID: auth.ID, ID: ctx.Params("notifierId"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.NotifierResponse]{Data: response})
}

func (c *NotifierController) Update(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	request := new(model.UpdateNotifierRequest)
	if err := ctx.BodyParser(request); err != nil {
		return fiber.ErrBadRequest
	}
	request.UserID = auth.ID
	request.ID = ctx.Params("notifierId")

	response, err := c.UseCase.Update(ctx.UserContext(), request)
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.NotifierResponse]{Data: response})
}

func (c *NotifierController) Delete(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.Delete(ctx.UserContext(), &model.GetNotifierRequest{
		UserID: auth.ID, ID: ctx.Params("notifierId"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[bool]{Data: response})
}

func (c *NotifierController) Verify(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	request := new(model.VerifyNotifierRequest)
	// an empty body means "issue me a code", so a parse failure is not fatal
	_ = ctx.BodyParser(request)
	request.UserID = auth.ID
	request.ID = ctx.Params("notifierId")

	response, err := c.UseCase.Verify(ctx.UserContext(), request)
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.VerifyNotifierResponse]{Data: response})
}

func (c *NotifierController) Test(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.Test(ctx.UserContext(), &model.GetNotifierRequest{
		UserID: auth.ID, ID: ctx.Params("notifierId"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.TestNotifierResponse]{Data: response})
}

// TelegramWebhook is public: the Bot API calls it. It always answers 200 so
// Telegram does not retry, whatever we made of the payload.
func (c *NotifierController) TelegramWebhook(ctx *fiber.Ctx) error {
	request := new(model.TelegramWebhookRequest)
	if err := ctx.BodyParser(request); err != nil {
		return ctx.JSON(model.WebResponse[bool]{Data: true})
	}

	if _, err := c.UseCase.HandleTelegramUpdate(ctx.UserContext(), request); err != nil {
		c.Log.WithError(err).Warn("Failed to handle a telegram update")
	}

	return ctx.JSON(model.WebResponse[bool]{Data: true})
}
