package http

import (
	"mailpulse/internal/delivery/http/middleware"
	"mailpulse/internal/model"
	"mailpulse/internal/usecase"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

type UserController struct {
	Log     *logrus.Logger
	UseCase *usecase.UserUseCase
}

func NewUserController(useCase *usecase.UserUseCase, log *logrus.Logger) *UserController {
	return &UserController{Log: log, UseCase: useCase}
}

func (c *UserController) Register(ctx *fiber.Ctx) error {
	request := new(model.RegisterUserRequest)
	if err := ctx.BodyParser(request); err != nil {
		return fiber.ErrBadRequest
	}

	response, err := c.UseCase.Create(ctx.UserContext(), request)
	if err != nil {
		return err
	}

	return ctx.Status(fiber.StatusCreated).JSON(model.WebResponse[*model.UserResponse]{Data: response})
}

func (c *UserController) Login(ctx *fiber.Ctx) error {
	request := new(model.LoginUserRequest)
	if err := ctx.BodyParser(request); err != nil {
		return fiber.ErrBadRequest
	}

	request.UserAgent = ctx.Get("User-Agent")
	request.IP = clientIP(ctx)

	response, err := c.UseCase.Login(ctx.UserContext(), request)
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.LoginResponse]{Data: response})
}

func (c *UserController) Logout(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.Logout(ctx.UserContext(), &model.LogoutUserRequest{
		ID:        auth.ID,
		SessionID: auth.SessionID,
		Token:     middleware.GetToken(ctx),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[bool]{Data: response})
}

func (c *UserController) Current(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.Current(ctx.UserContext(), &model.GetUserRequest{ID: auth.ID})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.UserResponse]{Data: response})
}

func (c *UserController) Update(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	request := new(model.UpdateUserRequest)
	if err := ctx.BodyParser(request); err != nil {
		return fiber.ErrBadRequest
	}
	request.ID = auth.ID

	response, err := c.UseCase.Update(ctx.UserContext(), request)
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.UserResponse]{Data: response})
}

func (c *UserController) ListSessions(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.ListSessions(ctx.UserContext(), &model.ListSessionRequest{
		UserID:           auth.ID,
		CurrentSessionID: auth.SessionID,
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[[]model.SessionResponse]{Data: response})
}

func (c *UserController) RevokeSession(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.RevokeSession(ctx.UserContext(), &model.RevokeSessionRequest{
		UserID:    auth.ID,
		SessionID: ctx.Params("sessionId"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[bool]{Data: response})
}

func (c *UserController) ForgotPassword(ctx *fiber.Ctx) error {
	request := new(model.ForgotPasswordRequest)
	if err := ctx.BodyParser(request); err != nil {
		return fiber.ErrBadRequest
	}

	response, err := c.UseCase.ForgotPassword(ctx.UserContext(), request)
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[bool]{Data: response})
}

func (c *UserController) ResetPassword(ctx *fiber.Ctx) error {
	request := new(model.ResetPasswordRequest)
	if err := ctx.BodyParser(request); err != nil {
		return fiber.ErrBadRequest
	}

	response, err := c.UseCase.ResetPassword(ctx.UserContext(), request)
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[bool]{Data: response})
}
