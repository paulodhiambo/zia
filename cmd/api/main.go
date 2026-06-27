package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"zia/internal/api"
	"zia/internal/connector"
	"zia/internal/connector/kcb"
	"zia/internal/connector/mpesa"
	"zia/internal/connector/paystack"
	"zia/internal/idempotency"
	"zia/internal/ledger"
	"zia/internal/orchestrator"
	"zia/internal/repository"
	"zia/internal/risk"
	"zia/internal/routing"
	"zia/internal/service"
	"zia/internal/telemetry"
	"zia/internal/webhook"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type config struct {
	port        string
	databaseURL string
	redisURL    string
	natsURL     string
	hmacSecret  string
	telemetry   telemetry.Config
}

func loadConfig() config {
	return config{
		port:        getEnv("PORT", "8080"),
		databaseURL: getEnv("DATABASE_URL", "postgres://zia:zia@localhost:5432/zia?sslmode=disable"),
		redisURL:    getEnv("REDIS_URL", "redis://localhost:6379/0"),
		natsURL:     getEnv("NATS_URL", "nats://localhost:4222"),
		hmacSecret:  getEnv("HMAC_SIGNING_SECRET", "dev-secret-do-not-use-in-production"),
		telemetry: telemetry.Config{
			Endpoint:    getEnv("OO_ENDPOINT", "http://localhost:5080"),
			Username:    getEnv("OO_EMAIL", "admin@zia.dev"),
			Password:    getEnv("OO_PASSWORD", ""),
			ServiceName: getEnv("OO_SERVICE_NAME", "zia-api"),
			Environment: getEnv("OO_ENVIRONMENT", "development"),
			SampleRate:  1.0,
		},
	}
}

func main() {
	cfg := loadConfig()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shutdown, err := telemetry.Setup(ctx, cfg.telemetry)
	if err != nil {
		log.Fatalf("failed to setup telemetry: %v", err)
	}
	defer shutdown(context.Background())

	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Sync()
	zap.ReplaceGlobals(logger)

	rdb := redis.NewClient(&redis.Options{Addr: cfg.redisURL})
	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Warn("redis not available, continuing without cache", zap.Error(err))
	}

	piRepo := repository.NewPaymentIntent(nil)
	attRepo := repository.NewAttempt(nil)
	whRepo := repository.NewWebhookEvent(nil)
	ledRepo := repository.NewLedger(nil)
	idempotencyStore := idempotency.NewStore(rdb)
	riskEng := risk.NewEngine()
	cb := routing.NewCircuitBreaker()
	routingEng := routing.NewEngine(cb, logger)

	ledgerEng := ledger.NewEngine(ledRepo)

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

	dedupStore := webhook.NewDedupStore(rdb)

	nc, err := nats.Connect(cfg.natsURL)
	if err != nil {
		logger.Warn("nats not available, continuing without event bus", zap.Error(err))
	}
	var js nats.JetStreamContext
	if nc != nil {
		js, _ = nc.JetStream()
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
	)

	webhookProc := webhook.NewProcessor(orc, js, logger)

	piSvc := service.NewPaymentIntent(orc)

	piHandler := api.NewPaymentIntentHandler(piSvc)
	whHandler := api.NewWebhookHandler(registry, whRepo, dedupStore, webhookProc, logger)

	router := api.NewRouter(api.Dependencies{
		Logger:        logger,
		PIHandler:     piHandler,
		WebhookHandler: whHandler,
	})

	srv := &http.Server{
		Addr:         ":" + cfg.port,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("server starting", zap.String("port", cfg.port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server error", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Fatal("server shutdown error", zap.Error(err))
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
