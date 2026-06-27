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
	"zia/internal/telemetry"

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

	router := api.NewRouter(api.Dependencies{
		Logger: logger,
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
