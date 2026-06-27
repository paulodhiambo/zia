package domain

import "time"

type WebhookProcessingStatus string

const (
	WebhookReceived   WebhookProcessingStatus = "received"
	WebhookProcessed  WebhookProcessingStatus = "processed"
	WebhookFailed     WebhookProcessingStatus = "failed"
	WebhookIgnored    WebhookProcessingStatus = "ignored"
)

type WebhookEvent struct {
	ID                string                   `db:"id" json:"id"`
	PSP               string                   `db:"psp" json:"psp"`
	EventType         string                   `db:"event_type" json:"eventType"`
	PSPReference      string                   `db:"psp_reference" json:"pspReference"`
	DedupKey          string                   `db:"dedup_key" json:"dedupKey"`
	Payload           []byte                   `db:"payload" json:"-"`
	ProcessingStatus  WebhookProcessingStatus  `db:"processing_status" json:"processingStatus"`
	ReceivedAt        time.Time                `db:"received_at" json:"receivedAt"`
}
