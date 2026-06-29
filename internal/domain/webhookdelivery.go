package domain

import "time"

type WebhookDeliveryStatus string

const (
	DeliveryPending   WebhookDeliveryStatus = "pending"
	DeliveryDelivered WebhookDeliveryStatus = "delivered"
	DeliveryFailed    WebhookDeliveryStatus = "failed"
)

type WebhookDelivery struct {
	ID              string                `db:"id" json:"id"`
	WebhookEventID  *string               `db:"webhook_event_id" json:"webhookEventId,omitempty"`
	EndpointID      string                `db:"endpoint_id" json:"endpointId"`
	URL             string                `db:"url" json:"url"`
	Status          WebhookDeliveryStatus `db:"status" json:"status"`
	RequestHeaders  []byte                `db:"request_headers" json:"-"`
	RequestBody     []byte                `db:"request_body" json:"-"`
	ResponseStatus  int                   `db:"response_status" json:"responseStatus"`
	ResponseBody    []byte                `db:"response_body" json:"-"`
	DurationMs      int                   `db:"duration_ms" json:"durationMs"`
	Attempt         int                   `db:"attempt" json:"attempt"`
	MaxAttempts     int                   `db:"max_attempts" json:"maxAttempts"`
	NextRetryAt     *time.Time            `db:"next_retry_at" json:"nextRetryAt,omitempty"`
	CreatedAt       time.Time             `db:"created_at" json:"createdAt"`
	UpdatedAt       time.Time             `db:"updated_at" json:"updatedAt"`
}
