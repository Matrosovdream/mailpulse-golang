package http

import (
	"mailpulse/internal/delivery/http/middleware"
	"mailpulse/internal/model"
	"mailpulse/internal/usecase"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

type ActivityController struct {
	Log     *logrus.Logger
	UseCase *usecase.ActivityUseCase
}

func NewActivityController(useCase *usecase.ActivityUseCase, log *logrus.Logger) *ActivityController {
	return &ActivityController{Log: log, UseCase: useCase}
}

func (c *ActivityController) ListMatches(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, metadata, err := c.UseCase.ListMatches(ctx.UserContext(), &model.ListMatchedEmailRequest{
		PageRequest:   paging(ctx),
		UserID:        auth.ID,
		WatcherID:     ctx.Query("watcher_id"),
		MailAccountID: ctx.Query("mail_account_id"),
		From:          queryMillis(ctx, "from"),
		To:            queryMillis(ctx, "to"),
		Query:         ctx.Query("q"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[[]model.MatchedEmailResponse]{Data: response, Paging: metadata})
}

func (c *ActivityController) GetMatch(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.GetMatch(ctx.UserContext(), &model.GetMatchedEmailRequest{
		UserID: auth.ID, ID: ctx.Params("matchId"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.MatchedEmailResponse]{Data: response})
}

func (c *ActivityController) ListRuns(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, metadata, err := c.UseCase.ListRuns(ctx.UserContext(), &model.ListEventRunRequest{
		PageRequest: paging(ctx),
		UserID:      auth.ID,
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

func (c *ActivityController) GetRun(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.GetRun(ctx.UserContext(), &model.GetEventRunRequest{
		UserID: auth.ID, ID: ctx.Params("runId"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.EventRunResponse]{Data: response})
}

func (c *ActivityController) RetryRun(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.Retry(ctx.UserContext(), &model.EventRunActionRequest{
		UserID: auth.ID, ID: ctx.Params("runId"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.EventRunResponse]{Data: response})
}

func (c *ActivityController) CancelRun(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.Cancel(ctx.UserContext(), &model.EventRunActionRequest{
		UserID: auth.ID, ID: ctx.Params("runId"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.CancelRunResponse]{Data: response})
}

func (c *ActivityController) AckRun(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.Ack(ctx.UserContext(), &model.EventRunActionRequest{
		UserID: auth.ID, ID: ctx.Params("runId"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.AckRunResponse]{Data: response})
}

func (c *ActivityController) ListDeliveries(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, metadata, err := c.UseCase.ListDeliveries(ctx.UserContext(), &model.ListDeliveryRequest{
		PageRequest: paging(ctx),
		UserID:      auth.ID,
		EventRunID:  ctx.Query("event_run_id"),
		NotifierID:  ctx.Query("notifier_id"),
		Status:      ctx.Query("status"),
	})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[[]model.DeliveryResponse]{Data: response, Paging: metadata})
}

func (c *ActivityController) DashboardSummary(ctx *fiber.Ctx) error {
	auth := middleware.GetUser(ctx)

	response, err := c.UseCase.Dashboard(ctx.UserContext(), &model.DashboardSummaryRequest{UserID: auth.ID})
	if err != nil {
		return err
	}

	return ctx.JSON(model.WebResponse[*model.DashboardSummaryResponse]{Data: response})
}
