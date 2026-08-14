package route

import (
	"mailpulse/internal/delivery/http"
	"mailpulse/internal/delivery/http/middleware"
	"mailpulse/internal/entity"

	"github.com/gofiber/fiber/v2"
)

// RouteConfig is the whole HTTP surface in one file, split into three groups:
// public, authenticated, and superadmin. The split is structural rather than
// per-handler, so an endpoint cannot be reached by URL guessing just because
// someone forgot a check inside a controller.
type RouteConfig struct {
	App *fiber.App

	UserController         *http.UserController
	MailAccountController  *http.MailAccountController
	NotifierController     *http.NotifierController
	WatcherController      *http.WatcherController
	WatcherEventController *http.WatcherEventController
	ActivityController     *http.ActivityController
	AdminController        *http.AdminController
	CatalogController      *http.CatalogController

	AuthMiddleware fiber.Handler
}

func (c *RouteConfig) Setup() {
	c.SetupPublicRoute()
	c.SetupAuthRoute()
	c.SetupAdminRoute()
}

// SetupPublicRoute is everything reachable without a token.
func (c *RouteConfig) SetupPublicRoute() {
	c.App.Get("/api/health", c.CatalogController.Health)

	c.App.Post("/api/users", c.UserController.Register)
	c.App.Post("/api/users/_login", c.UserController.Login)
	c.App.Post("/api/users/_forgot-password", c.UserController.ForgotPassword)
	c.App.Post("/api/users/_reset-password", c.UserController.ResetPassword)

	// provider callbacks: authenticated by their own state or secret token,
	// not by a session
	c.App.Get("/api/mail-accounts/oauth/:provider/_callback", c.MailAccountController.OAuthCallback)
	c.App.Post("/api/webhooks/telegram", c.NotifierController.TelegramWebhook)
}

// SetupAuthRoute is everything a signed-in user can reach.
func (c *RouteConfig) SetupAuthRoute() {
	api := c.App.Group("/api", c.AuthMiddleware)

	// ---- account and sessions
	api.Delete("/users", c.UserController.Logout)
	api.Get("/users/_current", c.UserController.Current)
	api.Patch("/users/_current", c.UserController.Update)
	api.Get("/users/_sessions", c.UserController.ListSessions)
	api.Delete("/users/_sessions/:sessionId", c.UserController.RevokeSession)

	// ---- catalogs that drive the SPA's dynamic forms
	api.Get("/event-types", c.CatalogController.EventTypes)
	api.Get("/notifier-types", c.CatalogController.NotifierTypes)
	api.Get("/filter-fields", c.CatalogController.FilterFields)

	// ---- mail accounts
	api.Get("/mail-accounts", c.MailAccountController.List)
	api.Post("/mail-accounts", c.MailAccountController.Create)
	api.Get("/mail-accounts/oauth/:provider/_authorize", c.MailAccountController.OAuthAuthorize)
	api.Get("/mail-accounts/:accountId", c.MailAccountController.Get)
	api.Patch("/mail-accounts/:accountId", c.MailAccountController.Update)
	api.Delete("/mail-accounts/:accountId", c.MailAccountController.Delete)
	api.Post("/mail-accounts/:accountId/_verify", c.MailAccountController.Verify)
	api.Post("/mail-accounts/:accountId/_sync", c.MailAccountController.Sync)
	api.Get("/mail-accounts/:accountId/folders", c.MailAccountController.Folders)
	api.Get("/mail-accounts/:accountId/sync-runs", c.MailAccountController.SyncRuns)

	// ---- notifiers
	api.Get("/notifiers", c.NotifierController.List)
	api.Post("/notifiers", c.NotifierController.Create)
	api.Get("/notifiers/:notifierId", c.NotifierController.Get)
	api.Patch("/notifiers/:notifierId", c.NotifierController.Update)
	api.Delete("/notifiers/:notifierId", c.NotifierController.Delete)
	api.Post("/notifiers/:notifierId/_verify", c.NotifierController.Verify)
	api.Post("/notifiers/:notifierId/_test", c.NotifierController.Test)

	// ---- watchers
	api.Get("/watchers", c.WatcherController.List)
	api.Post("/watchers", c.WatcherController.Create)
	api.Get("/watchers/:watcherId", c.WatcherController.Get)
	api.Patch("/watchers/:watcherId", c.WatcherController.Update)
	api.Delete("/watchers/:watcherId", c.WatcherController.Delete)
	api.Post("/watchers/:watcherId/_archive", c.WatcherController.Archive)
	api.Post("/watchers/:watcherId/_restore", c.WatcherController.Restore)
	api.Post("/watchers/:watcherId/_pause", c.WatcherController.Pause)
	api.Post("/watchers/:watcherId/_resume", c.WatcherController.Resume)
	api.Post("/watchers/:watcherId/_test", c.WatcherController.Test)
	api.Get("/watchers/:watcherId/stats", c.WatcherController.Stats)

	// ---- filters, replaced as a set because the UI edits them as one form
	api.Get("/watchers/:watcherId/filters", c.WatcherController.GetFilters)
	api.Put("/watchers/:watcherId/filters", c.WatcherController.ReplaceFilters)

	// ---- events
	api.Get("/watchers/:watcherId/events", c.WatcherEventController.List)
	api.Post("/watchers/:watcherId/events", c.WatcherEventController.Create)
	api.Post("/watchers/:watcherId/events/_reorder", c.WatcherEventController.Reorder)
	api.Get("/watchers/:watcherId/events/:eventId", c.WatcherEventController.Get)
	api.Patch("/watchers/:watcherId/events/:eventId", c.WatcherEventController.Update)
	api.Delete("/watchers/:watcherId/events/:eventId", c.WatcherEventController.Delete)
	api.Post("/watchers/:watcherId/events/:eventId/_test", c.WatcherEventController.Test)

	// ---- activity and logs
	api.Get("/matches", c.ActivityController.ListMatches)
	api.Get("/matches/:matchId", c.ActivityController.GetMatch)
	api.Get("/event-runs", c.ActivityController.ListRuns)
	api.Get("/event-runs/:runId", c.ActivityController.GetRun)
	api.Post("/event-runs/:runId/_retry", c.ActivityController.RetryRun)
	api.Post("/event-runs/:runId/_cancel", c.ActivityController.CancelRun)
	api.Post("/event-runs/:runId/_ack", c.ActivityController.AckRun)
	api.Get("/deliveries", c.ActivityController.ListDeliveries)
	api.Get("/dashboard/summary", c.ActivityController.DashboardSummary)
}

// SetupAdminRoute mounts the superadmin group behind its own role check.
func (c *RouteConfig) SetupAdminRoute() {
	admin := c.App.Group("/api/admin", c.AuthMiddleware, middleware.NewRequireRole(entity.RoleSuperadmin))

	admin.Get("/stats", c.AdminController.Stats)
	admin.Get("/users", c.AdminController.ListUsers)
	admin.Get("/users/:userId", c.AdminController.GetUser)
	admin.Patch("/users/:userId", c.AdminController.UpdateUser)
	admin.Post("/users/:userId/_suspend", c.AdminController.SuspendUser)
	admin.Post("/users/:userId/_restore", c.AdminController.RestoreUser)
	admin.Post("/users/:userId/_impersonate", c.AdminController.Impersonate)
	admin.Get("/watchers", c.AdminController.ListWatchers)
	admin.Get("/mail-accounts", c.AdminController.ListMailAccounts)
	admin.Get("/notifiers", c.AdminController.ListNotifiers)
	admin.Get("/event-runs", c.AdminController.ListRuns)
	admin.Get("/audit-logs", c.AdminController.ListAuditLogs)
}
