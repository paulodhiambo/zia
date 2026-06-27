package pesalink

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"zia/internal/connector"
)

type Config struct {
	APIKey       string
	APISecret    string
	PartnerID    string
	CallbackURL  string
	AllowedIPs   []string
	Sandbox      bool
}

type Connector struct {
	config Config
	http   *http.Client
	auth   *TokenManager
}

func New(cfg Config) *Connector {
	return &Connector{
		config: cfg,
		http:   &http.Client{Timeout: 30 * time.Second},
		auth:   NewTokenManager(cfg),
	}
}

func (c *Connector) Name() string { return "pesalink" }

func (c *Connector) Capabilities() connector.Capabilities {
	return connector.Capabilities{
		SupportsCollection:  false,
		SupportsPayout:      true,
		SupportsRefund:      false,
		SupportedCurrencies: []string{"KES", "UGX", "TZS", "RWF", "USD"},
		SupportedCountries:  []string{"KE", "UG", "TZ", "RW"},
		ConfirmationStyle:   "webhook_poll",
	}
}

func (c *Connector) baseURL() string {
	if c.config.Sandbox {
		return "https://sandbox.api.pesalink.com"
	}
	return "https://api.pesalink.com"
}

func (c *Connector) InitiateCollection(ctx context.Context, req connector.CollectionRequest) (connector.CollectionResult, error) {
	return connector.CollectionResult{}, fmt.Errorf("pesalink: collection not supported")
}

func (c *Connector) GetStatus(ctx context.Context, pspReference string) (connector.StatusResult, error) {
	token, err := c.auth.Token(ctx)
	if err != nil {
		return connector.StatusResult{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET",
		c.baseURL()+"/v1/transfers/"+pspReference, nil)
	if err != nil {
		return connector.StatusResult{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return connector.StatusResult{}, fmt.Errorf("pesalink status: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var statusResp struct {
		Status   string `json:"status"`
		Amount   int64  `json:"amount"`
		Currency string `json:"currency"`
	}
	if err := json.Unmarshal(body, &statusResp); err != nil {
		return connector.StatusResult{}, fmt.Errorf("parse pesalink status: %w", err)
	}

	mapped := "pending"
	switch statusResp.Status {
	case "completed", "settled":
		mapped = "succeeded"
	case "failed", "rejected", "expired":
		mapped = "failed"
	}

	return connector.StatusResult{
		Supported:   true,
		Status:      mapped,
		AmountMinor: statusResp.Amount,
		Currency:    statusResp.Currency,
	}, nil
}

func (c *Connector) Refund(ctx context.Context, req connector.RefundRequest) (connector.RefundResult, error) {
	return connector.RefundResult{}, fmt.Errorf("pesalink: refund not supported")
}

func (c *Connector) InitiatePayout(ctx context.Context, req connector.PayoutRequest) (connector.PayoutResult, error) {
	token, err := c.auth.Token(ctx)
	if err != nil {
		return connector.PayoutResult{}, err
	}

	quote, err := c.createQuote(ctx, token, req)
	if err != nil {
		return connector.PayoutResult{}, fmt.Errorf("pesalink quote: %w", err)
	}

	recipientID, err := c.resolveRecipient(ctx, token, req)
	if err != nil {
		return connector.PayoutResult{}, fmt.Errorf("pesalink recipient: %w", err)
	}

	transferID, err := c.transfer(ctx, token, req, quote, recipientID)
	if err != nil {
		return connector.PayoutResult{}, fmt.Errorf("pesalink transfer: %w", err)
	}

	return connector.PayoutResult{
		PSPReference: transferID,
		Status:       "accepted",
	}, nil
}

func (c *Connector) createQuote(ctx context.Context, token string, req connector.PayoutRequest) (string, error) {
	body := map[string]any{
		"source_currency": req.Currency,
		"target_currency": req.TargetCurrency,
		"amount":          req.AmountMinor,
		"partner_id":      c.config.PartnerID,
	}
	data, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL()+"/v1/quotes",
		bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Idempotency-Key", req.IdempotencyKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("pesalink quote failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var quoteResp struct {
		QuoteID string `json:"quote_id"`
	}
	if err := json.Unmarshal(respBody, &quoteResp); err != nil {
		return "", fmt.Errorf("parse quote response: %w", err)
	}

	return quoteResp.QuoteID, nil
}

func (c *Connector) resolveRecipient(ctx context.Context, token string, req connector.PayoutRequest) (string, error) {
	body := map[string]any{
		"bank_account_ref": req.BankAccountRef,
		"merchant_id":      req.MerchantID,
		"currency":         req.TargetCurrency,
	}
	data, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL()+"/v1/recipients/lookup",
		bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("pesalink recipient lookup failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var lookupResp struct {
		PesalinkAcctID string `json:"pesalink_acct_id"`
	}
	if err := json.Unmarshal(respBody, &lookupResp); err != nil {
		return "", fmt.Errorf("parse recipient lookup: %w", err)
	}

	if lookupResp.PesalinkAcctID == "" {
		return "", fmt.Errorf("pesalink: no recipient found for ref=%s", req.BankAccountRef)
	}

	return lookupResp.PesalinkAcctID, nil
}

func (c *Connector) transfer(ctx context.Context, token string, req connector.PayoutRequest, quoteID, recipientID string) (string, error) {
	body := map[string]any{
		"quote_id":      quoteID,
		"recipient_id":  recipientID,
		"amount":        req.AmountMinor,
		"currency":      req.Currency,
		"target_currency": req.TargetCurrency,
		"reference":     "merchant settlement",
		"callback_url":  c.config.CallbackURL,
	}
	data, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL()+"/v1/transfers",
		bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Idempotency-Key", req.IdempotencyKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("pesalink transfer failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var transferResp struct {
		TransferID string `json:"transfer_id"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal(respBody, &transferResp); err != nil {
		return "", fmt.Errorf("parse transfer response: %w", err)
	}

	return transferResp.TransferID, nil
}

func (c *Connector) ParseWebhook(ctx context.Context, headers map[string]string, body []byte) (connector.WebhookEvent, error) {
	var event struct {
		Event string          `json:"event"`
		Data  json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		return connector.WebhookEvent{}, fmt.Errorf("parse pesalink webhook: %w", err)
	}

	switch event.Event {
	case "transfer.completed":
		var transfer struct {
			TransferID string `json:"transfer_id"`
			Amount     int64  `json:"amount"`
			Currency   string `json:"currency"`
			Status     string `json:"status"`
			Reference  string `json:"reference"`
		}
		if err := json.Unmarshal(event.Data, &transfer); err != nil {
			return connector.WebhookEvent{}, fmt.Errorf("parse transfer.completed: %w", err)
		}

		return connector.WebhookEvent{
			PSP:          "pesalink",
			EventType:    "transfer.completed",
			PSPReference: transfer.TransferID,
			DedupKey:     "pesalink:transfer.completed:" + transfer.TransferID,
			Status:       "succeeded",
			AmountMinor:  transfer.Amount,
			Currency:     transfer.Currency,
			RawPayload:   body,
		}, nil

	case "transfer.failed":
		var transfer struct {
			TransferID string `json:"transfer_id"`
			Amount     int64  `json:"amount"`
			Currency   string `json:"currency"`
			Reason     string `json:"reason"`
		}
		if err := json.Unmarshal(event.Data, &transfer); err != nil {
			return connector.WebhookEvent{}, fmt.Errorf("parse transfer.failed: %w", err)
		}

		return connector.WebhookEvent{
			PSP:          "pesalink",
			EventType:    "transfer.failed",
			PSPReference: transfer.TransferID,
			DedupKey:     "pesalink:transfer.failed:" + transfer.TransferID,
			Status:       "failed",
			AmountMinor:  transfer.Amount,
			Currency:     transfer.Currency,
			RawPayload:   body,
		}, nil

	default:
		return connector.WebhookEvent{
			PSP:        "pesalink",
			EventType:  event.Event,
			Status:     "unknown",
			RawPayload: body,
		}, nil
	}
}
