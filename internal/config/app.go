package config

import (
	"strings"
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
	"mailpulse/internal/gateway/oauth"
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
	Redis    *redis.Client
	Producer sarama.SyncProducer
}

// Container holds the wired usecases the worker needs. The web server only
// needs the routes, but cmd/worker drives the pipeline and dispatcher directly.
type Container struct {
	Pipeline     *usecase.PipelineUseCase
	Dispatcher   *usecase.DispatcherUseCase
	MailAccounts *usecase.MailAccountUseCase

	// Catalog is exposed for its Health check and nothing else. cmd/worker
	// cannot serve the route table it was given — Bootstrap registers all 75
	// routes on whatever fiber app it is handed, so listening on that one
	// would serve the whole API, admin included, from the worker container.
	// Its health port mounts this directly instead.
	Catalog *usecase.CatalogUseCase

	// Providers is exposed so a tool can substitute the mail client after
	// wiring — cmd/loadtest swaps in the load stub, because measuring the
	// pipeline against a real mailbox measures the mailbox. Nothing in the
	// web or worker binaries touches it.
	Providers *mail.Registry
}

// Bootstrap wires the process in stages: each one takes the bundles the
// earlier stages produced. The bundles are unexported and exist only to keep
// the layers named — the whole graph is still built once, at startup, with no
// runtime lookup anywhere.
func Bootstrap(config *BootstrapConfig) *Container {
	cipher, err := secret.NewCipher(config.Config.GetString("security.encryption_key"))
	if err != nil {
		config.Log.Fatalf("Cannot start without credential encryption: %v", err)
	}

	repos := newRepositories(config.Log)
	caches := newCaches(config)
	registries := newRegistries(config, cipher)
	useCases := newUseCases(config, cipher, repos, caches, registries)

	setupRoutes(config, useCases, registries.Providers, caches.RateLimiter)

	return &Container{
		Pipeline:     useCases.Pipeline,
		Dispatcher:   useCases.Dispatcher,
		MailAccounts: useCases.MailAccount,
		Catalog:      useCases.Catalog,
		Providers:    registries.Providers,
	}
}

// ---------------------------------------------------------------- repositories

type repositories struct {
	Users         *repository.UserRepository
	Roles         *repository.RoleRepository
	Sessions      *repository.UserSessionRepository
	Accounts      *repository.MailAccountRepository
	MailProviders *repository.MailProviderRepository
	Notifiers     *repository.NotifierRepository
	Watchers      *repository.WatcherRepository
	Filters       *repository.WatcherFilterRepository
	WatcherEvents *repository.WatcherEventRepository
	Matches       *repository.MatchedEmailRepository
	Runs          *repository.EventRunRepository
	Deliveries    *repository.NotificationDeliveryRepository
	SyncRuns      *repository.MailSyncRunRepository
	AuditLogs     *repository.AuditLogRepository
}

// Repositories hold no connection: the *gorm.DB is passed per call, so these
// are a logger apiece and the whole set costs a couple of hundred bytes.
func newRepositories(log *logrus.Logger) repositories {
	return repositories{
		Users:         repository.NewUserRepository(log),
		Roles:         repository.NewRoleRepository(log),
		Sessions:      repository.NewUserSessionRepository(log),
		Accounts:      repository.NewMailAccountRepository(log),
		MailProviders: repository.NewMailProviderRepository(log),
		Notifiers:     repository.NewNotifierRepository(log),
		Watchers:      repository.NewWatcherRepository(log),
		Filters:       repository.NewWatcherFilterRepository(log),
		WatcherEvents: repository.NewWatcherEventRepository(log),
		Matches:       repository.NewMatchedEmailRepository(log),
		Runs:          repository.NewEventRunRepository(log),
		Deliveries:    repository.NewNotificationDeliveryRepository(log),
		SyncRuns:      repository.NewMailSyncRunRepository(log),
		AuditLogs:     repository.NewAuditLogRepository(log),
	}
}

// ---------------------------------------------------------------- caches

type caches struct {
	Users         *cache.UserCache
	PasswordReset *cache.PasswordResetCache
	OAuthStates   *cache.OAuthStateCache
	MailProviders *cache.MailProviderCache
	RateLimiter   *cache.RateLimiter
	Locks         *cache.Lock
}

func newCaches(config *BootstrapConfig) caches {
	seconds := func(key string) time.Duration {
		return time.Duration(config.Config.GetInt(key)) * time.Second
	}

	return caches{
		Users:         cache.NewUserCache(config.Redis, config.Log, seconds("redis.ttl.auth")),
		PasswordReset: cache.NewPasswordResetCache(config.Redis, config.Log, seconds("redis.ttl.password_reset")),
		OAuthStates:   cache.NewOAuthStateCache(config.Redis, config.Log, seconds("redis.ttl.oauth_state")),
		MailProviders: cache.NewMailProviderCache(config.Redis, config.Log, seconds("redis.ttl.mail_provider")),
		RateLimiter:   cache.NewRateLimiter(config.Redis, config.Log),
		Locks:         cache.NewLock(config.Redis, config.Log),
	}
}

// ---------------------------------------------------------------- registries

type registries struct {
	Channels  *gwnotifier.Registry
	Handlers  *event.Registry
	OAuth     *oauth.Registry
	Providers *mail.Registry
}

// Adding a delivery medium or an event type means writing one implementation
// and adding it here. Routes, controllers and the SPA are untouched, because
// the config forms are rendered from ConfigSchema.
func newRegistries(config *BootstrapConfig, cipher *secret.Cipher) registries {
	channels := gwnotifier.NewRegistry(config.Log)
	channels.Register(
		gwnotifier.NewTelegramChannel(config.Config.GetString("telegram.bot_token")),
		gwnotifier.NewSMSChannel(),
		gwnotifier.NewEmailChannel(),
		gwnotifier.NewWebhookChannel(),
		gwnotifier.NewSlackChannel(),
		gwnotifier.NewDiscordChannel(),
	)

	handlers := event.NewRegistry(config.Log)
	handlers.Register(
		event.NewNotifyHandler(channels, cipher, config.Config.GetString("app.base_url")),
		event.NewWebhookHandler(),
		event.NewHTTPRequestHandler(),
	)

	return registries{
		Channels:  channels,
		Handlers:  handlers,
		OAuth:     newOAuthRegistry(config),
		Providers: newMailRegistry(config),
	}
}

// OAuth clients are keyed by provider slug, not by kind: which consent screen a
// mailbox uses is a property of the service, and gmail, outlook and yandex all
// sit on the imap kind. A provider whose id and secret are unset registers
// nothing, which is what makes _authorize answer 501 for it instead of sending
// the user to a screen that would turn them away.
func newOAuthRegistry(config *BootstrapConfig) *oauth.Registry {
	callbackBase := config.Config.GetString("oauth.callback_base_url")

	clients := oauth.NewRegistry()
	clients.Register(
		oauth.NewGoogle(oauth.Config{
			ClientID:     config.Config.GetString("oauth.google.client_id"),
			ClientSecret: config.Config.GetString("oauth.google.client_secret"),
		}, callbackBase),
		oauth.NewMicrosoft(oauth.Config{
			ClientID:     config.Config.GetString("oauth.microsoft.client_id"),
			ClientSecret: config.Config.GetString("oauth.microsoft.client_secret"),
			Tenant:       config.Config.GetString("oauth.microsoft.tenant"),
		}, callbackBase),
		oauth.NewYandex(oauth.Config{
			ClientID:     config.Config.GetString("oauth.yandex.client_id"),
			ClientSecret: config.Config.GetString("oauth.yandex.client_secret"),
		}, callbackBase),
	)

	// development and test escape hatch: run the whole flow against a server we
	// control, because a real one needs an app registration that takes weeks to
	// approve. Only the endpoints are faked; the exchange, refresh and storage
	// are the production path.
	if stubURL := config.Config.GetString("oauth.stub_url"); stubURL != "" {
		config.Log.Warnf("OAUTH_STUB_URL is set: yandex OAuth is faked against %s, no real provider is contacted", stubURL)
		clients.Register(oauth.NewStub(oauth.YandexSlug, stubURL))
	}

	if slugs := clients.Slugs(); len(slugs) > 0 {
		config.Log.Infof("OAuth is configured for %s", strings.Join(slugs, ", "))
	}

	return clients
}

// Clients are registered by kind. Which providers a user may pick, and the
// host/port presets they get, are rows in mail_providers — so adding Fastmail
// is a seed row, not a code change.
func newMailRegistry(config *BootstrapConfig) *mail.Registry {
	providers := mail.NewRegistry()

	if config.Config.GetBool("mail.stub_enabled") {
		// development escape hatch: synthesises messages instead of connecting
		config.Log.Warn("MAIL_STUB_ENABLED is on: mailboxes are faked, no IMAP server will be contacted")
		providers.Register(stubmail.NewStubProvider(config.Log))
		return providers
	}

	providers.Register(imapmail.NewIMAPProvider(config.Log,
		time.Duration(config.Config.GetInt("mail.imap_timeout"))*time.Second))

	return providers
}

// ---------------------------------------------------------------- usecases

type useCases struct {
	Audit        *usecase.AuditUseCase
	User         *usecase.UserUseCase
	Resolver     *usecase.MailResolver
	Pipeline     *usecase.PipelineUseCase
	Dispatcher   *usecase.DispatcherUseCase
	MailAccount  *usecase.MailAccountUseCase
	Notifier     *usecase.NotifierUseCase
	WatcherEvent *usecase.WatcherEventUseCase
	Watcher      *usecase.WatcherUseCase
	Activity     *usecase.ActivityUseCase
	Admin        *usecase.AdminUseCase
	Catalog      *usecase.CatalogUseCase
}

// Order matters here and only here: the dispatcher is built before the two
// usecases that hand it a run to execute, so both take it as a constructor
// argument and neither is ever observable with a nil dispatcher.
func newUseCases(config *BootstrapConfig, cipher *secret.Cipher,
	repos repositories, caches caches, registries registries) useCases {

	var userProducer *messaging.UserProducer
	if config.Producer != nil {
		userProducer = messaging.NewUserProducer(config.Producer, config.Log)
	}

	baseURL := config.Config.GetString("app.base_url")

	audit := usecase.NewAuditUseCase(config.DB, config.Log, config.Validate, repos.AuditLogs)

	user := usecase.NewUserUseCase(config.DB, config.Log, config.Validate,
		repos.Users, repos.Roles, repos.Sessions, audit, userProducer,
		caches.Users, caches.PasswordReset,
		time.Duration(config.Config.GetInt("session.ttl"))*time.Second)

	resolver := usecase.NewMailResolver(config.DB, repos.MailProviders, repos.Accounts,
		registries.Providers, registries.OAuth, cipher, caches.Locks,
		caches.MailProviders, config.Log)

	pipeline := usecase.NewPipelineUseCase(config.DB, config.Log, repos.Accounts,
		repos.Watchers, repos.Filters, repos.WatcherEvents, repos.Matches, repos.Runs,
		repos.SyncRuns, registries.Providers, cipher, resolver)

	dispatcher := usecase.NewDispatcherUseCase(config.DB, config.Log, repos.Runs,
		repos.Deliveries, repos.Matches, repos.Watchers, repos.WatcherEvents,
		registries.Handlers)

	mailAccount := usecase.NewMailAccountUseCase(config.DB, config.Log, config.Validate,
		repos.Accounts, repos.MailProviders, repos.Watchers, repos.SyncRuns,
		registries.Providers, cipher, audit, pipeline, resolver,
		registries.OAuth, caches.OAuthStates, baseURL)

	notifier := usecase.NewNotifierUseCase(config.DB, config.Log, config.Validate,
		repos.Notifiers, registries.Channels, cipher, audit)

	watcherEvent := usecase.NewWatcherEventUseCase(config.DB, config.Log, config.Validate,
		repos.Watchers, repos.WatcherEvents, repos.Notifiers, repos.Matches, repos.Runs,
		repos.Deliveries, registries.Handlers, dispatcher, audit)

	watcher := usecase.NewWatcherUseCase(config.DB, config.Log, config.Validate,
		repos.Watchers, repos.Filters, repos.WatcherEvents, repos.Accounts,
		repos.Matches, repos.Runs, watcherEvent, audit)

	activity := usecase.NewActivityUseCase(config.DB, config.Log, config.Validate,
		repos.Matches, repos.Runs, repos.Deliveries, repos.WatcherEvents,
		repos.Watchers, repos.Accounts, repos.Notifiers, dispatcher, audit)

	admin := usecase.NewAdminUseCase(config.DB, config.Log, config.Validate,
		repos.Users, repos.Roles, repos.Sessions, repos.Watchers, repos.Accounts,
		repos.Notifiers, repos.Matches, repos.Runs, audit, user)

	catalog := usecase.NewCatalogUseCase(config.DB, config.Redis, config.Log,
		registries.Handlers, registries.Channels, registries.Providers,
		repos.MailProviders, config.Config.GetString("app.version"))

	return useCases{
		Audit: audit, User: user, Resolver: resolver, Pipeline: pipeline,
		Dispatcher: dispatcher, MailAccount: mailAccount, Notifier: notifier,
		WatcherEvent: watcherEvent, Watcher: watcher, Activity: activity,
		Admin: admin, Catalog: catalog,
	}
}

// ---------------------------------------------------------------- http

func setupRoutes(config *BootstrapConfig, useCases useCases,
	providers *mail.Registry, limiter *cache.RateLimiter) {
	rateLimit := func(name, attemptsKey, windowKey string) fiber.Handler {
		return middleware.NewRateLimit(limiter, name,
			config.Config.GetInt(attemptsKey),
			time.Duration(config.Config.GetInt(windowKey))*time.Second)
	}

	routeConfig := route.RouteConfig{
		App:            config.App,
		UserController: http.NewUserController(useCases.User, config.Log),
		MailAccountController: http.NewMailAccountController(
			useCases.MailAccount, config.Log),
		NotifierController: http.NewNotifierController(useCases.Notifier, config.Log),
		WatcherController: http.NewWatcherController(useCases.Watcher,
			providers, useCases.MailAccount, config.Log),
		WatcherEventController: http.NewWatcherEventController(
			useCases.WatcherEvent, config.Log),
		ActivityController: http.NewActivityController(useCases.Activity, config.Log),
		CatalogController:  http.NewCatalogController(useCases.Catalog, config.Log),
		AdminController: http.NewAdminController(useCases.Admin, useCases.Activity,
			useCases.Audit, useCases.Watcher, useCases.MailAccount,
			useCases.Notifier, config.Log),
		DocsController: http.NewDocsController(),
		DocsEnabled:    config.Config.GetBool("web.docs_enabled"),
		CORSOrigins:    splitAndTrim(config.Config.GetString("web.cors_origins")),
		AuthMiddleware: middleware.NewAuth(useCases.User),
		LoginRateLimit: rateLimit("login",
			"security.ratelimit.login.attempts", "security.ratelimit.login.window"),
		ForgotPasswordRateLimit: rateLimit("forgot-password",
			"security.ratelimit.forgot_password.attempts",
			"security.ratelimit.forgot_password.window"),
	}

	routeConfig.Setup()
}
