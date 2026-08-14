package http

import (
	"mailpulse/internal/delivery/http/middleware"
	"mailpulse/internal/entity"
	"mailpulse/internal/model"
	"mailpulse/internal/usecase"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

// AdminController serves the superadmin group. Every handler here is mounted
// behind NewRequireRole(superadmin); none of them re-checks the role.
type AdminController struct {
	Log      *logrus.Logger
	UseCase  *usecase.AdminUseCase
	Activity *usecase.ActivityUseCase
	Audit    *usecase.AuditUseCase
	Watchers *usecase.WatcherUseCase
	Accounts *usecase.MailAccountUseCase
	Notifier *usecase.NotifierUseCase
}

func NewAdminController(useCase *usecase.AdminUseCase, activity *usecase.ActivityUseCase,
	audit *usecase.AuditUseCase, watchers *usecase.WatcherUseCase,
	accounts *usecase.MailAccountUseCase, notifier *usecase.NotifierUseCase,
	log *logrus.Logger) *AdminController {
	return &AdminController{
		Log: log, UseCase: useCase, Activity: activity, Audit: audit,
		Watchers: watchers, Accounts: accounts, Notifier: notifier,
	}
}

func (c *AdminController) Stats(ctx *fiber.Ctx) error {
	response, err := c.UseCase.Stats(ctx.UserContext())
	if err != nil {
		return err
	}
	return ctx.JSON(model.WebResponse[*model.AdminStatsResponse]{Data: response})
}

func (c *AdminController) ListUsers(ctx *fiber.Ctx) error {
	response, metadata, err := c.UseCase.ListUsers(ctx.UserContext(), &model.ListAdminUserRequest{
		PageRequest: paging(ctx),
		Query:       ctx.Query("q"),
		Role:        ctx.Query("role"),
		Status:      ctx.Query("status"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[[]model.AdminUserResponse]{Data: response, Paging: metadata})
}

func (c *AdminController) GetUser(ctx *fiber.Ctx) error {
	response, err := c.UseCase.GetUser(ctx.UserContext(), &model.GetAdminUserRequest{ID: ctx.Params("userId")})
	if err != nil {
		return err
	}
	return ctx.JSON(model.WebResponse[*model.AdminUserResponse]{Data: response})
}

func (c *AdminController) UpdateUser(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	request := new(model.UpdateAdminUserRequest)
	if err := ctx.BodyParser(request); err != nil {
		return fiber.ErrBadRequest
	}
	request.ActorID = auth.ID
	request.ID = ctx.Params("userId")

	response, err := c.UseCase.UpdateUser(ctx.UserContext(), request)
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.AdminUserResponse]{Data: response})
}

func (c *AdminController) setStatus(ctx *fiber.Ctx, status string) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.SetUserStatus(ctx.UserContext(), &model.SetAdminUserStatusRequest{
		ActorID: auth.ID, ID: ctx.Params("userId"), Status: status,
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.SuspendUserResponse]{Data: response})
}

func (c *AdminController) SuspendUser(ctx *fiber.Ctx) error {
	return c.setStatus(ctx, entity.UserStatusSuspended)
}

func (c *AdminController) RestoreUser(ctx *fiber.Ctx) error {
	return c.setStatus(ctx, entity.UserStatusActive)
}

func (c *AdminController) Impersonate(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.Impersonate(ctx.UserContext(), &model.ImpersonateRequest{
		ActorID:   auth.ID,
		ID:        ctx.Params("userId"),
		UserAgent: ctx.Get("User-Agent"),
		IP:        clientIP(ctx),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.ImpersonateResponse]{Data: response})
}

// ListWatchers and its neighbours reuse the user-facing usecases with the
// owner filter left empty, or set from ?user_id=.
func (c *AdminController) ListWatchers(ctx *fiber.Ctx) error {
	response, metadata, err := c.Watchers.List(ctx.UserContext(), &model.ListWatcherRequest{
		PageRequest:   paging(ctx),
		UserID:        ctx.Query("user_id"),
		Status:        ctx.Query("status"),
		MailAccountID: ctx.Query("mail_account_id"),
		Query:         ctx.Query("q"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[[]model.WatcherResponse]{Data: response, Paging: metadata})
}

func (c *AdminController) ListMailAccounts(ctx *fiber.Ctx) error {
	response, metadata, err := c.Accounts.List(ctx.UserContext(), &model.ListMailAccountRequest{
		PageRequest: paging(ctx),
		UserID:      ctx.Query("user_id"),
		Status:      ctx.Query("status"),
		Provider:    ctx.Query("provider"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[[]model.MailAccountResponse]{Data: response, Paging: metadata})
}

func (c *AdminController) ListNotifiers(ctx *fiber.Ctx) error {
	response, metadata, err := c.Notifier.List(ctx.UserContext(), &model.ListNotifierRequest{
		PageRequest: paging(ctx),
		UserID:      ctx.Query("user_id"),
		Type:        ctx.Query("type"),
		Status:      ctx.Query("status"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[[]model.NotifierResponse]{Data: response, Paging: metadata})
}

func (c *AdminController) ListRuns(ctx *fiber.Ctx) error {
	response, metadata, err := c.Activity.ListRuns(ctx.UserContext(), &model.ListEventRunRequest{
		PageRequest: paging(ctx),
		UserID:      ctx.Query("user_id"),
		WatcherID:   ctx.Query("watcher_id"),
		Status:      ctx.Query("status"),
		From:        queryMillis(ctx, "from"),
		To:          queryMillis(ctx, "to"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[[]model.EventRunResponse]{Data: response, Paging: metadata})
}

func (c *AdminController) ListAuditLogs(ctx *fiber.Ctx) error {
	response, metadata, err := c.Audit.List(ctx.UserContext(), &model.ListAuditLogRequest{
		PageRequest: paging(ctx),
		ActorUserID: ctx.Query("actor_user_id"),
		Action:      ctx.Query("action"),
		EntityType:  ctx.Query("entity_type"),
		From:        queryMillis(ctx, "from"),
		To:          queryMillis(ctx, "to"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[[]model.AuditLogResponse]{Data: response, Paging: metadata})
}
