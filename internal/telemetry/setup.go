package telemetry

import (
	"context"
	"encoding/base64"
	"errors"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

type Config struct {
	Endpoint    string
	Username    string
	Password    string
	ServiceName string
	Environment string
	SampleRate  float64
}

// quietErrorHandler suppresses "connection refused" noise when the OTel
// backend (OpenObserve) isn't running. All other errors are logged once.
// Without this the SDK prints a line every 15 s (one per metric flush) to
// stderr, which drowns out real log output in local dev.
type quietErrorHandler struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func (h *quietErrorHandler) Handle(err error) {
	msg := err.Error()
	if strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.seen[msg]; !ok {
		h.seen[msg] = struct{}{}
		log.Printf("otel: %v", err)
	}
}

// endpointParts parses cfg.Endpoint (a full URL like "http://localhost:5080")
// and returns the host:port string and whether the scheme is plain HTTP.
// WithEndpoint on OTLP HTTP exporters expects host:port, not a full URL.
func endpointParts(endpoint string) (host string, insecure bool, err error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", false, err
	}
	return u.Host, u.Scheme == "http", nil
}

func authHeader(username, password string) map[string]string {
	if username == "" {
		return nil
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	return map[string]string{"Authorization": "Basic " + encoded}
}

func Setup(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error) {
	otel.SetErrorHandler(&quietErrorHandler{seen: make(map[string]struct{})})

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			attribute.String("environment", cfg.Environment),
		),
	)
	if err != nil {
		return nil, err
	}

	var shutdownFuncs []func(context.Context) error

	shutdown = func(ctx context.Context) error {
		var errs []error
		for _, fn := range shutdownFuncs {
			errs = append(errs, fn(ctx))
		}
		return errors.Join(errs...)
	}

	tp, err := newTraceProvider(ctx, cfg, res)
	if err != nil {
		return nil, err
	}
	shutdownFuncs = append(shutdownFuncs, tp.Shutdown)
	otel.SetTracerProvider(tp)

	mp, err := newMeterProvider(ctx, cfg, res)
	if err != nil {
		return nil, err
	}
	shutdownFuncs = append(shutdownFuncs, mp.Shutdown)
	otel.SetMeterProvider(mp)

	lp, err := newLoggerProvider(ctx, cfg, res)
	if err != nil {
		return nil, err
	}
	shutdownFuncs = append(shutdownFuncs, lp.Shutdown)
	global.SetLoggerProvider(lp)

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return shutdown, nil
}

func newTraceProvider(ctx context.Context, cfg Config, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	host, insecure, err := endpointParts(cfg.Endpoint)
	if err != nil {
		return nil, err
	}

	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(host),
		otlptracehttp.WithURLPath("/api/default/v1/traces"),
		otlptracehttp.WithHeaders(authHeader(cfg.Username, cfg.Password)),
	}
	if insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exp, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRate))),
	)
	return tp, nil
}

func newMeterProvider(ctx context.Context, cfg Config, res *resource.Resource) (*sdkmetric.MeterProvider, error) {
	host, insecure, err := endpointParts(cfg.Endpoint)
	if err != nil {
		return nil, err
	}

	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(host),
		otlpmetrichttp.WithURLPath("/api/default/v1/metrics"),
		otlpmetrichttp.WithHeaders(authHeader(cfg.Username, cfg.Password)),
	}
	if insecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}

	exp, err := otlpmetrichttp.New(ctx, opts...)
	if err != nil {
		return nil, err
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp,
			sdkmetric.WithInterval(15*time.Second))),
		sdkmetric.WithResource(res),
	)
	return mp, nil
}

func newLoggerProvider(ctx context.Context, cfg Config, res *resource.Resource) (*sdklog.LoggerProvider, error) {
	host, insecure, err := endpointParts(cfg.Endpoint)
	if err != nil {
		return nil, err
	}

	opts := []otlploghttp.Option{
		otlploghttp.WithEndpoint(host),
		otlploghttp.WithURLPath("/api/default/v1/logs"),
		otlploghttp.WithHeaders(authHeader(cfg.Username, cfg.Password)),
	}
	if insecure {
		opts = append(opts, otlploghttp.WithInsecure())
	}

	exp, err := otlploghttp.New(ctx, opts...)
	if err != nil {
		return nil, err
	}

	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exp)),
		sdklog.WithResource(res),
	)
	return lp, nil
}
