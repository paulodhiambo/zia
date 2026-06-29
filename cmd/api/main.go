package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"zia/internal/api"
	"zia/internal/authn"
	"zia/internal/connector"
	"zia/internal/connector/mpesa"
	"zia/internal/connector/paystack"
	"zia/internal/idempotency"
	"zia/internal/ledger"
	"zia/internal/notification"
	"zia/internal/orchestrator"
	"zia/internal/repository"
	"zia/internal/risk"
	"zia/internal/routing"
	"zia/internal/service"
	"zia/internal/telemetry"
	"zia/internal/webhook"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
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
			SampleRate:  getEnvFloat("OO_SAMPLE_RATE", 1.0),
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

	var telemetryShutdown func(context.Context) error
	var setupErr error
	if cfg.telemetry.Endpoint != "" {
		telemetryShutdown, setupErr = telemetry.Setup(ctx, cfg.telemetry)
		if setupErr != nil {
			log.Fatalf("failed to setup telemetry: %v", setupErr)
		}
	} else {
		log.Println("telemetry disabled (no endpoint configured)")
	}
	defer func() {
		if telemetryShutdown != nil {
			if err := telemetryShutdown(context.Background()); err != nil {
				log.Printf("telemetry shutdown: %v", err)
			}
		}
	}()

	baseLogger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("failed to create logger: %v", err)
	}
	logger := telemetry.BridgeLogger(baseLogger, cfg.telemetry.ServiceName)
	defer func() { _ = logger.Sync() }()
	zap.ReplaceGlobals(logger)

	rdbOpts, err := redis.ParseURL(cfg.redisURL)
	if err != nil {
		logger.Warn("invalid redis URL, using default", zap.String("url", cfg.redisURL), zap.Error(err))
		rdbOpts = &redis.Options{Addr: "localhost:6379"}
	}
	rdb := redis.NewClient(rdbOpts)
	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Warn("redis not available, continuing without cache", zap.Error(err))
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.databaseURL)
	if err != nil {
		logger.Fatal("failed to parse database URL", zap.Error(err))
	}
	poolCfg.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		logger.Fatal("failed to create pool", zap.Error(err))
	}
	defer pool.Close()
	// verify connectivity eagerly so the first request doesn't pay for a
	// cold-start connection handshake (which can exceed the HTTP write timeout).
	if err := pool.Ping(ctx); err != nil {
		logger.Fatal("database unreachable", zap.Error(err))
	}
	logger.Info("database connected")

	piRepo := repository.NewPaymentIntent(pool)
	attRepo := repository.NewAttempt(pool)
	whRepo := repository.NewWebhookEvent(pool)
	merchantRepo := repository.NewMerchant(pool)
	payoutRepo := repository.NewPayout(pool)
	checkoutRepo := repository.NewCheckout(pool)
	ledRepo := repository.NewLedger(pool)
	userRepo := repository.NewUserRepo(pool)
	sessionRepo := repository.NewSessionRepo(pool)
	customerRepo := repository.NewCustomerRepo(pool)
	teamMemberRepo := repository.NewTeamMemberRepo(pool)
	teamInviteRepo := repository.NewTeamInvitationRepo(pool)
	webhookEPRepo := repository.NewWebhookEndpointRepo(pool)
	notifRepo := repository.NewNotificationRepo(pool)
	idempotencyStore := idempotency.NewStore(rdb)
	riskEng := risk.NewEngine()
	cb := routing.NewCircuitBreaker()
	routingEng := routing.NewEngine(cb, logger)

	ledgerEng := ledger.NewEngine(ledRepo)

	registry := connector.NewRegistry()

	if cfg := mpesa.ConfigFromEnv(); cfg.ConsumerKey != "" {
		registry.Register("mpesa", mpesa.New(cfg, logger))
		logger.Info("registered mpesa connector")
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

	var notifDispatcher *notification.Dispatcher
	if js != nil {
		notifDispatcher = notification.NewDispatcher(merchantRepo, notifRepo, js, logger)
	}

	feePercent, _ := strconv.Atoi(os.Getenv("PLATFORM_FEE_PERCENT"))
	if feePercent <= 0 {
		feePercent = 5
	}
	feeMin, _ := strconv.Atoi(os.Getenv("PLATFORM_FEE_MIN"))
	if feeMin <= 0 {
		feeMin = 100
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
		orchestrator.FeeConfig{
			Percent:   feePercent,
			MinAmount: int64(feeMin),
		},
	)

	webhookProc := webhook.NewProcessor(orc, js, logger)

	piSvc := service.NewPaymentIntent(orc)

	piHandler := api.NewPaymentIntentHandler(piSvc, logger)
	whHandler := api.NewWebhookHandler(registry, whRepo, dedupStore, webhookProc, logger)
	merchantHandler := api.NewMerchantHandler(merchantRepo, piRepo, payoutRepo, ledRepo, logger)
	checkoutHandler := api.NewCheckoutHandler(piSvc, checkoutRepo, piRepo, logger)
	portalHandler := api.NewPortalHandler(
		userRepo, sessionRepo, merchantRepo, piRepo, attRepo, payoutRepo, ledRepo,
		customerRepo, teamMemberRepo, teamInviteRepo, webhookEPRepo, whRepo, notifRepo, logger,
	)
	authMiddleware := authn.Middleware(merchantRepo)
	sessionMiddleware := api.PortalAuth(sessionRepo)

	router := api.NewRouter(api.Dependencies{
		Logger:            logger,
		PIHandler:         piHandler,
		WebhookHandler:    whHandler,
		MerchantHandler:   merchantHandler,
		CheckoutHandler:   checkoutHandler,
		PortalHandler:     portalHandler,
		AuthMiddleware:    authMiddleware,
		SessionMiddleware: sessionMiddleware,
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

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}
