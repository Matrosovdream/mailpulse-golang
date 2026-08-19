package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mailpulse/internal/config"
	"mailpulse/internal/delivery/messaging"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func main() {
	viperConfig := config.NewViper()
	logger := config.NewLogger(viperConfig)
	logger.Info("Starting worker service")

	db := config.NewDatabase(viperConfig, logger)
	redisClient := config.NewRedis(viperConfig, logger)
	validate := config.NewValidator(viperConfig)
	producer := config.NewKafkaProducer(viperConfig, logger)

	// the worker builds the same container as the web server and simply never
	// listens; sharing Bootstrap keeps one wiring path rather than two that
	// drift apart
	container := config.Bootstrap(&config.BootstrapConfig{
		DB:       db,
		App:      fiber.New(),
		Log:      logger,
		Validate: validate,
		Config:   viperConfig,
		Producer: producer,
		Redis:    redisClient,
	})

	ctx, cancel := context.WithCancel(context.Background())

	go runPoller(ctx, logger, viperConfig, container)
	go runDispatcher(ctx, logger, viperConfig, container)
	go runCredentialChecker(ctx, logger, viperConfig, container)
	go runUserConsumer(ctx, logger, viperConfig)

	terminate := make(chan os.Signal, 1)
	signal.Notify(terminate, syscall.SIGINT, syscall.SIGTERM)

	received := <-terminate
	logger.Infof("Got %s, shutting the worker down gracefully", received)
	cancel()

	time.Sleep(5 * time.Second) // let in-flight work finish
}

// runPoller drives the mail side: claim the mailboxes whose poll is due, fetch,
// match, and schedule the resulting events.
func runPoller(ctx context.Context, log *logrus.Logger, viperConfig *viper.Viper, container *config.Container) {
	interval := time.Duration(viperConfig.GetInt("worker.poll_interval")) * time.Second
	batch := viperConfig.GetInt("worker.poll_batch")

	log.Infof("Mail poller running every %s", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("Mail poller stopping")
			return
		case <-ticker.C:
			polled, err := container.Pipeline.PollDue(ctx, batch)
			if err != nil {
				log.WithError(err).Warn("Mail poll failed")
				continue
			}
			if polled > 0 {
				log.Infof("Polled %d mail accounts", polled)
			}
		}
	}
}

// runDispatcher drains event_runs. Several workers can run this concurrently:
// the claim uses SKIP LOCKED, so they share the queue instead of colliding.
func runDispatcher(ctx context.Context, log *logrus.Logger, viperConfig *viper.Viper, container *config.Container) {
	interval := time.Duration(viperConfig.GetInt("worker.dispatch_interval")) * time.Second
	batch := viperConfig.GetInt("worker.dispatch_batch")

	log.Infof("Event dispatcher running every %s", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("Event dispatcher stopping")
			return
		case <-ticker.C:
			handled, err := container.Dispatcher.Tick(ctx, batch)
			if err != nil {
				log.WithError(err).Warn("Dispatch tick failed")
				continue
			}
			if handled > 0 {
				log.Infof("Dispatched %d event runs", handled)
			}
		}
	}
}

// runCredentialChecker re-tests stored mailbox credentials on a schedule.
//
// App passwords get revoked and hosts change, and without this the first sign
// would be a watcher that quietly stopped firing. A failure marks the account
// error, which also removes it from the poll queue until it recovers.
func runCredentialChecker(ctx context.Context, log *logrus.Logger, viperConfig *viper.Viper, container *config.Container) {
	interval := time.Duration(viperConfig.GetInt("worker.verify_interval")) * time.Second
	olderThan := time.Duration(viperConfig.GetInt("mail.reverify_after")) * time.Second
	batch := viperConfig.GetInt("worker.verify_batch")

	log.Infof("Credential checker running every %s, re-checking anything older than %s", interval, olderThan)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("Credential checker stopping")
			return
		case <-ticker.C:
			checked, failed, err := container.MailAccounts.ReverifyDue(ctx, olderThan, batch)
			if err != nil {
				log.WithError(err).Warn("Credential check failed")
				continue
			}
			if checked > 0 {
				log.Infof("Re-checked %d mail accounts, %d failing", checked, failed)
			}
		}
	}
}

func runUserConsumer(ctx context.Context, log *logrus.Logger, viperConfig *viper.Viper) {
	if !viperConfig.GetBool("kafka.producer.enabled") {
		log.Info("Kafka is disabled, not starting the user consumer")
		return
	}

	log.Info("setup user consumer")
	group := config.NewKafkaConsumerGroup(viperConfig, log)
	handler := messaging.NewUserConsumer(log)
	messaging.ConsumeTopic(ctx, group, "users", log, handler.Consume)
}
