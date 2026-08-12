package config

import (
	"mailpulse/internal/delivery/http"
	"mailpulse/internal/delivery/http/middleware"
	"mailpulse/internal/delivery/http/route"
	"mailpulse/internal/gateway/cache"
	"mailpulse/internal/gateway/messaging"
	"mailpulse/internal/repository"
	"mailpulse/internal/usecase"
	"time"

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

func Bootstrap(config *BootstrapConfig) {
	// setup repositories
	userRepository := repository.NewUserRepository(config.Log)

	// setup caches
	authTTL := time.Duration(config.Config.GetInt("redis.ttl.auth")) * time.Second
	userCache := cache.NewUserCache(config.Redis, config.Log, authTTL)

	// setup producer
	var userProducer *messaging.UserProducer

	if config.Producer != nil {
		userProducer = messaging.NewUserProducer(config.Producer, config.Log)
	}

	// setup use cases
	userUseCase := usecase.NewUserUseCase(config.DB, config.Log, config.Validate, userRepository, userProducer, userCache)

	// setup controller
	userController := http.NewUserController(userUseCase, config.Log)

	// setup middleware
	authMiddleware := middleware.NewAuth(userUseCase)

	routeConfig := route.RouteConfig{
		App:            config.App,
		UserController: userController,
		AuthMiddleware: authMiddleware,
	}
	routeConfig.Setup()
}
