package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"zia/internal/connector"
	"zia/internal/connector/kcb"
	"zia/internal/connector/mpesa"
	"zia/internal/connector/paystack"
	"zia/internal/connector/pesalink"
	"zia/internal/idempotency"
	"zia/internal/ledger"
	"zia/internal/notification"
	"zia/internal/orchestrator"
	"zia/internal/repository"
	"zia/internal/risk"
	"zia/internal/routing"
	"zia/internal/telemetry"
	"zia/internal/webhook"

	"github.com/joho/godotenv"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type config struct {
	natsURL     string
	databaseURL string
	redisURL    string
	telemetry   telemetry.Config
}

func loadConfig() config {
	return config{
		natsURL:     getEnv("NATS_URL", "nats://localhost:4222"),
		databaseURL: getEnv("DATABASE_URL", "postgres://zia:zia@localhost:5432/zia?sslmode=disable"),
		redisURL:    getEnv("REDIS_URL", "redis://localhost:6379/0"),
		telemetry: telemetry.Config{
			Endpoint:    getEnv("OO_ENDPOINT", "http://localhost:5080"),
			Username:    getEnv("OO_EMAIL", "admin@zia.dev"),
			Password:    getEnv("OO_PASSWORD", ""),
			ServiceName: getEnv("OO_SERVICE_NAME", "zia-worker"),
			Environment: getEnv("OO_ENVIRONMENT", "development"),
			SampleRate:  1.0,
		},
	}
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Printf("warning: no .env file found: %v", err)
	}
	cfg := loadConfig()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shutdown, err := telemetry.Setup(ctx, cfg.telemetry)
	if err != nil {
		log.Fatalf("failed to setup telemetry: %v", err)
	}
	defer func() {
		if err := shutdown(context.Background()); err != nil {
			log.Printf("telemetry shutdown: %v", err)
		}
	}()

	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("failed to create logger: %v", err)
	}
	defer func() { _ = logger.Sync() }()

	rdb := redis.NewClient(&redis.Options{Addr: cfg.redisURL})
	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Warn("redis not available, continuing without cache", zap.Error(err))
	}

	nc, err := nats.Connect(cfg.natsURL)
	if err != nil {
		logger.Fatal("failed to connect to nats", zap.Error(err))
	}
	defer nc.Close()

	js, err := nc.JetStream()
	if err != nil {
		logger.Fatal("failed to create jetstream context", zap.Error(err))
	}

	if _, err := js.AddStream(&nats.StreamConfig{
		Name:     "zia",
		Subjects: []string{"zia.>"},
	}); err != nil {
		logger.Warn("stream may already exist", zap.Error(err))
	}

	piRepo := repository.NewPaymentIntent(nil)
	attRepo := repository.NewAttempt(nil)
	ledRepo := repository.NewLedger(nil)
	idempotencyStore := idempotency.NewStore(rdb)
	riskEng := risk.NewEngine()
	cb := routing.NewCircuitBreaker()
	routingEng := routing.NewEngine(cb, logger)
	ledgerEng := ledger.NewEngine(ledRepo)
	merchantRepo := repository.NewMerchant(nil)
	notifDispatcher := notification.NewDispatcher(merchantRepo, js, logger)

	registry := connector.NewRegistry()

	if cfg := mpesa.ConfigFromEnv(); cfg.ConsumerKey != "" {
		registry.Register("mpesa", mpesa.New(cfg))
		logger.Info("registered mpesa connector")
	}
	if cfg := kcb.ConfigFromEnv(); cfg.ConsumerKey != "" {
		registry.Register("kcb", kcb.New(cfg))
		logger.Info("registered kcb connector")
	}
	if cfg := paystack.ConfigFromEnv(); cfg.SecretKey != "" {
		registry.Register("paystack", paystack.New(cfg))
		logger.Info("registered paystack connector")
	}
	if cfg := pesalink.ConfigFromEnv(); cfg.APIKey != "" {
		registry.Register("pesalink", pesalink.New(cfg))
		logger.Info("registered pesalink connector")
	}

	orc := orchestrator.New(
		piRepo,
		attRepo,
		registry,
		routingEng,
		riskEng,
		idempotencyStore,
		logger,
		ledgerEng,
		notifDispatcher,
	)

	processor := webhook.NewProcessor(orc, js, logger)

	if err := notifDispatcher.StartConsumer(ctx); err != nil {
		logger.Fatal("failed to start notification consumer", zap.Error(err))
	}

	if err := processor.StartConsumer(ctx); err != nil {
		logger.Fatal("failed to start consumer", zap.Error(err))
	}

	logger.Info("worker started, consuming from zia.webhook.received and zia.notification.>")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("worker shutting down")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
