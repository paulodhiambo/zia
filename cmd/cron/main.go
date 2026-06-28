package main

import (
	"context"
	"log"
	"os"
	"time"

	"zia/internal/connector"
	"zia/internal/connector/kcb"
	"zia/internal/connector/mpesa"
	"zia/internal/connector/paystack"
	"zia/internal/connector/pesalink"
	"zia/internal/ledger"
	"zia/internal/reconciliation"
	"zia/internal/repository"
	"zia/internal/settlement"
	"zia/internal/telemetry"

	"github.com/joho/godotenv"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

type config struct {
	databaseURL string
	telemetry   telemetry.Config
}

func loadConfig() config {
	return config{
		databaseURL: getEnv("DATABASE_URL", "postgres://zia:zia@localhost:5432/zia?sslmode=disable"),
		telemetry: telemetry.Config{
			Endpoint:    getEnv("OO_ENDPOINT", "http://localhost:5080"),
			Username:    getEnv("OO_EMAIL", "admin@zia.dev"),
			Password:    getEnv("OO_PASSWORD", ""),
			ServiceName: getEnv("OO_SERVICE_NAME", "zia-cron"),
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
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

	pool, err := pgxpool.New(ctx, cfg.databaseURL)
	if err != nil {
		logger.Fatal("failed to connect to database", zap.Error(err))
	}
	defer pool.Close()

	attRepo := repository.NewAttempt(pool)
	merchantRepo := repository.NewMerchant(pool)
	payoutRepo := repository.NewPayout(pool)
	ledRepo := repository.NewLedger(pool)
	ledgerEng := ledger.NewEngine(ledRepo)

	registry := connector.NewRegistry()

	if cfg := mpesa.ConfigFromEnv(); cfg.ConsumerKey != "" {
		registry.Register("mpesa", mpesa.New(cfg, logger))
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

	recon := reconciliation.NewRunner(registry, attRepo, logger)

	yesterday := time.Now().UTC().Add(-24 * time.Hour)
	if err := recon.Reconcile(ctx, yesterday); err != nil {
		logger.Error("reconciliation failed", zap.Error(err))
	} else {
		logger.Info("reconciliation completed")
	}

	settler := settlement.NewRunner(merchantRepo, payoutRepo, ledgerEng, registry, logger)
	if err := settler.Settle(ctx, settlement.DefaultPolicy); err != nil {
		logger.Fatal("settlement run failed", zap.Error(err))
	}

	logger.Info("settlement completed")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
