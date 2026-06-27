package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"zia/internal/telemetry"

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

	logger.Info("cron worker starting")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("cron worker shutting down")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
