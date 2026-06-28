package telemetry

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var meter = otel.Meter("zia")

var (
	PaymentAttempts, _             = meter.Int64Counter("zia.payment.attempts")
	PaymentSucceeded, _            = meter.Int64Counter("zia.payment.succeeded")
	PaymentFailed, _               = meter.Int64Counter("zia.payment.failed")
	ConnectorLatency, _            = meter.Float64Histogram("zia.connector.latency_seconds", metric.WithUnit("s"))
	WebhookLag, _                  = meter.Float64Histogram("zia.webhook.processing_lag_seconds", metric.WithUnit("s"))
	LedgerImbalances, _            = meter.Int64Counter("zia.ledger.imbalances")
	TokenRefreshFailures, _        = meter.Int64Counter("zia.token_refresh.failures")
	ReconciliationExceptions, _    = meter.Int64Counter("zia.reconciliation.exceptions")
	CircuitBreakerOpen, _           = meter.Int64ObservableGauge("zia.circuit_breaker.open")
	DLQDepth, _                     = meter.Int64ObservableGauge("zia.dlq.depth")
)
