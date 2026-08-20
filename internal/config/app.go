package config

import (
	"time"

	"mailpulse/internal/delivery/http"
	"mailpulse/internal/delivery/http/middleware"
	"mailpulse/internal/delivery/http/route"
	"mailpulse/internal/gateway/cache"
	"mailpulse/internal/gateway/mail"
	imapmail "mailpulse/internal/gateway/mail/imap"
	stubmail "mailpulse/internal/gateway/mail/stub"
	"mailpulse/internal/gateway/messaging"
	gwnotifier "mailpulse/internal/gateway/notifier"
	"mailpulse/internal/gateway/secret"
	"mailpulse/internal/repository"
	"mailpulse/internal/usecase"
	"mailpulse/internal/usecase/event"

	"github.com/IBM/sarama"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

type BootstrapConfig struct {
	DB       *gorm.DB
	App      *fiber.App
	Log      *logrus.Logger
	Validate *validator.Validate
	Config   *viper.Viper
	Producer sarama.SyncProducer
	Redis    *redis.Client
}

// Container holds the wired usecases the worker needs. The web server only
// needs the routes, but cmd/worker drives the pipeline and dispatcher directly.
type Container struct {
	Pipeline     *usecase.PipelineUseCase
	Dispatcher   *usecase.DispatcherUseCase
	MailAccounts *usecase.MailAccountUseCase
}

func Bootstrap(config *BootstrapConfig) *Container {
	// ---------------------------------------------------------------- crypto
	cipher, err := secret.NewCipher(config.Config.GetString("security.encryption_key"))
	if err != nil {
		config.Log.Fatalf("Cannot start without credential encryption: %v", err)
	}

	// ---------------------------------------------------------------- repositories
	users := repository.NewUserRepository(config.Log)
	roles := repository.NewRoleRepository(config.Log)
	sessions := repository.NewUserSessionRepository(config.Log)
	accounts := repository.NewMailAccountRepository(config.Log)
	mailProviders := repository.NewMailProviderRepository(config.Log)
	notifiers := repository.NewNotifierRepository(config.Log)
	watchers := repository.NewWatcherRepository(config.Log)
	filters := repository.NewWatcherFilterRepository(config.Log)
	watcherEvents := repository.NewWatcherEventRepository(config.Log)
	matches := repository.NewMatchedEmailRepository(config.Log)
	runs := repository.NewEventRunRepository(config.Log)
	deliveries := repository.NewNotificationDeliveryRepository(config.Log)
	syncRuns := repository.NewMailSyncRunRepository(config.Log)
	auditLogs := repository.NewAuditLogRepository(config.Log)

	// ---------------------------------------------------------------- caches
	authTTL := time.Duration(config.Config.GetInt("redis.ttl.auth")) * time.Second
	resetTTL := time.Duration(config.Config.GetInt("redis.ttl.password_reset")) * time.Second
	userCache := cache.NewUserCache(config.Redis, config.Log, authTTL)
	resetCache := cache.NewPasswordResetCache(config.Redis, config.Log, resetTTL)
	rateLimiter := cache.NewRateLimiter(config.Redis, config.Log)

	// ---------------------------------------------------------------- producers
	var userProducer *messaging.UserProducer
	if config.Producer != nil {
		userProducer = messaging.NewUserProducer(config.Producer, config.Log)
	}

	// ---------------------------------------------------------------- registries
	//
	// Adding a delivery medium or an event type means writing one implementation
	// and adding it here. Routes, controllers and the SPA are untouched, because
	// the config forms are rendered from ConfigSchema.
	channels := gwnotifier.NewRegistry(config.Log)
	channels.Register(
		gwnotifier.NewTelegramChannel(config.Config.GetString("telegram.bot_token")),
		gwnotifier.NewSMSChannel(),
		gwnotifier.NewEmailChannel(),
		gwnotifier.NewWebhookChannel(),
		gwnotifier.NewSlackChannel(),
		gwnotifier.NewDiscordChannel(),
	)

	baseURL := config.Config.GetString("app.base_url")
	handlers := event.NewRegistry(config.Log)
	handlers.Register(
		event.NewNotifyHandler(channels, cipher, baseURL),
		event.NewWebhookHandler(),
		event.NewHTTPRequestHandler(),
	)

	// Clients are registered by kind. Which providers a user may pick, and the
	// host/port presets they get, are rows in mail_providers — so adding
	// Fastmail is a seed row, not a code change.
	providers := mail.NewRegistry()
	if config.Config.GetBool("mail.stub_enabled") {
		// development escape hatch: synthesises messages instead of connecting
		config.Log.Warn("MAIL_STUB_ENABLED is on: mailboxes are faked, no IMAP server will be contacted")
		providers.Register(stubmail.NewStubProvider(config.Log))
	} else {
		providers.Register(imapmail.NewIMAPProvider(config.Log,
			time.Duration(config.Config.GetInt("mail.imap_timeout"))*time.Second))
	}

	// ---------------------------------------------------------------- usecases
	audit := usecase.NewAuditUseCase(config.DB, config.Log, config.Validate, auditLogs)

	sessionTTL := time.Duration(config.Config.GetInt("session.ttl")) * time.Second
	userUseCase := usecase.NewUserUseCase(config.DB, config.Log, config.Validate,
		users, roles, sessions, audit, userProducer, userCache, resetCache, sessionTTL)

	mailResolver := usecase.NewMailResolver(mailProviders, providers, cipher)

	pipeline := usecase.NewPipelineUseCase(config.DB, config.Log, accounts, watchers,
		filters, watcherEvents, matches, runs, syncRuns, providers, cipher, mailResolver)

	dispatcher := usecase.NewDispatcherUseCase(config.DB, config.Log, runs, deliveries,
		matches, watchers, watcherEvents, handlers)

	mailAccountUseCase := usecase.NewMailAccountUseCase(config.DB, config.Log, config.Validate,
		accounts, mailProviders, watchers, syncRuns, providers, cipher, audit, pipeline, mailResolver)

	notifierUseCase := usecase.NewNotifierUseCase(config.DB, config.Log, config.Validate,
		notifiers, channels, cipher, audit)

	watcherEventUseCase := usecase.NewWatcherEventUseCase(config.DB, config.Log, config.Validate,
		watchers, watcherEvents, notifiers, matches, runs, handlers, audit)
	watcherEventUseCase.SetDispatcher(dispatcher)

	watcherUseCase := usecase.NewWatcherUseCase(config.DB, config.Log, config.Validate,
		watchers, filters, watcherEvents, accounts, matches, runs, watcherEventUseCase, audit)

	activityUseCase := usecase.NewActivityUseCase(config.DB, config.Log, config.Validate,
		matches, runs, deliveries, watcherEvents, watchers, accounts, notifiers, audit)
	activityUseCase.SetDispatcher(dispatcher)

	adminUseCase := usecase.NewAdminUseCase(config.DB, config.Log, config.Validate,
		users, roles, sessions, watchers, accounts, notifiers, matches, runs, audit, userUseCase)

	catalogUseCase := usecase.NewCatalogUseCase(config.DB, config.Redis, config.Log,
		handlers, channels, providers, mailProviders, config.Config.GetString("app.version"))

	// ---------------------------------------------------------------- controllers
	userController := http.NewUserController(userUseCase, config.Log)
	mailAccountController := http.NewMailAccountController(mailAccountUseCase, config.Log)
	notifierController := http.NewNotifierController(notifierUseCase, config.Log)
	watcherController := http.NewWatcherController(watcherUseCase, providers, mailAccountUseCase, config.Log)
	watcherEventController := http.NewWatcherEventController(watcherEventUseCase, config.Log)
	activityController := http.NewActivityController(activityUseCase, config.Log)
	catalogController := http.NewCatalogController(catalogUseCase, config.Log)
	adminController := http.NewAdminController(adminUseCase, activityUseCase, audit,
		watcherUseCase, mailAccountUseCase, notifierUseCase, config.Log)
	docsController := http.NewDocsController()

	// ---------------------------------------------------------------- routes
	routeConfig := route.RouteConfig{
		App:                    config.App,
		UserController:         userController,
		MailAccountController:  mailAccountController,
		NotifierController:     notifierController,
		WatcherController:      watcherController,
		WatcherEventController: watcherEventController,
		ActivityController:     activityController,
		AdminController:        adminController,
		CatalogController:      catalogController,
		DocsController:         docsController,
		DocsEnabled:            config.Config.GetBool("web.docs_enabled"),
		CORSOrigins:            splitAndTrim(config.Config.GetString("web.cors_origins")),
		AuthMiddleware:         middleware.NewAuth(userUseCase),
		LoginRateLimit: middleware.NewRateLimit(rateLimiter, "login",
			config.Config.GetInt("security.ratelimit.login.attempts"),
			time.Duration(config.Config.GetInt("security.ratelimit.login.window"))*time.Second),
		ForgotPasswordRateLimit: middleware.NewRateLimit(rateLimiter, "forgot-password",
			config.Config.GetInt("security.ratelimit.forgot_password.attempts"),
			time.Duration(config.Config.GetInt("security.ratelimit.forgot_password.window"))*time.Second),
	}
	routeConfig.Setup()

	return &Container{
		Pipeline:     pipeline,
		Dispatcher:   dispatcher,
		MailAccounts: mailAccountUseCase,
	}
}
