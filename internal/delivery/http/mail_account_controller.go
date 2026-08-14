package http

import (
	"mailpulse/internal/delivery/http/middleware"
	"mailpulse/internal/model"
	"mailpulse/internal/usecase"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

type MailAccountController struct {
	Log     *logrus.Logger
	UseCase *usecase.MailAccountUseCase
}

func NewMailAccountController(useCase *usecase.MailAccountUseCase, log *logrus.Logger) *MailAccountController {
	return &MailAccountController{Log: log, UseCase: useCase}
}

func (c *MailAccountController) List(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	request := &model.ListMailAccountRequest{
		PageRequest: paging(ctx),
		UserID:      auth.ID,
		Status:      ctx.Query("status"),
		Provider:    ctx.Query("provider"),
	}

	response, metadata, err := c.UseCase.List(ctx.UserContext(), request)
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[[]model.MailAccountResponse]{Data: response, Paging: metadata})
}

func (c *MailAccountController) Create(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	request := new(model.CreateMailAccountRequest)
	if err := ctx.BodyParser(request); err != nil {
		return fiber.ErrBadRequest
	}
	request.UserID = auth.ID

	response, err := c.UseCase.Create(ctx.UserContext(), request)
	if err != nil {
		return err
	}

	return ctx.Status(fiber.StatusCreated).JSON(model.WebResponse[*model.MailAccountResponse]{Data: response})
}

func (c *MailAccountController) Get(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.Get(ctx.UserContext(), &model.GetMailAccountRequest{
		UserID: auth.ID, ID: ctx.Params("accountId"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.MailAccountResponse]{Data: response})
}

func (c *MailAccountController) Update(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	request := new(model.UpdateMailAccountRequest)
	if err := ctx.BodyParser(request); err != nil {
		return fiber.ErrBadRequest
	}
	request.UserID = auth.ID
	request.ID = ctx.Params("accountId")

	response, err := c.UseCase.Update(ctx.UserContext(), request)
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.MailAccountResponse]{Data: response})
}

func (c *MailAccountController) Delete(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.Delete(ctx.UserContext(), &model.GetMailAccountRequest{
		UserID: auth.ID, ID: ctx.Params("accountId"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[bool]{Data: response})
}

func (c *MailAccountController) Verify(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.Verify(ctx.UserContext(), &model.GetMailAccountRequest{
		UserID: auth.ID, ID: ctx.Params("accountId"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.VerifyMailAccountResponse]{Data: response})
}

func (c *MailAccountController) Sync(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.Sync(ctx.UserContext(), &model.GetMailAccountRequest{
		UserID: auth.ID, ID: ctx.Params("accountId"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.SyncMailAccountResponse]{Data: response})
}

func (c *MailAccountController) Folders(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.Folders(ctx.UserContext(), &model.GetMailAccountRequest{
		UserID: auth.ID, ID: ctx.Params("accountId"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[[]model.FolderResponse]{Data: response})
}

func (c *MailAccountController) SyncRuns(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)
	page := paging(ctx)

	response, metadata, err := c.UseCase.ListSyncRuns(ctx.UserContext(), &model.GetMailAccountRequest{
		UserID: auth.ID, ID: ctx.Params("accountId"),
	}, &page)
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[[]model.MailSyncRunResponse]{Data: response, Paging: metadata})
}

func (c *MailAccountController) OAuthAuthorize(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.Authorize(ctx.UserContext(), &model.OAuthAuthorizeRequest{
		UserID: auth.ID, Provider: ctx.Params("provider"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.OAuthAuthorizeResponse]{Data: response})
}

func (c *MailAccountController) OAuthCallback(ctx *fiber.Ctx) error {
	response, err := c.UseCase.Callback(ctx.UserContext(), &model.OAuthCallbackRequest{
		Provider: ctx.Params("provider"),
		Code:     ctx.Query("code"),
		State:    ctx.Query("state"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.MailAccountResponse]{Data: response})
}
