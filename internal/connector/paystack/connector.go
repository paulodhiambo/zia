package paystack

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
	SecretKey  string
	PublicKey  string
	WebhookKey string
	Sandbox    bool
}

type Connector struct {
	config Config
	http   *http.Client
}

func New(cfg Config) *Connector {
	return &Connector{
		config: cfg,
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Connector) Name() string { return "paystack" }

func (c *Connector) Capabilities() connector.Capabilities {
	return connector.Capabilities{
		SupportsCollection:  true,
		SupportsPayout:      true,
		SupportsRefund:      true,
		SupportedCurrencies: []string{"NGN", "GHS", "ZAR", "USD"},
		SupportedCountries:  []string{"NG", "GH", "ZA"},
		ConfirmationStyle:   "webhook_poll",
	}
}

func (c *Connector) baseURL() string {
	if c.config.Sandbox {
		return "https://api.paystack.co" // Paystack uses the same domain; test keys route to sandbox
	}
	return "https://api.paystack.co"
}

func (c *Connector) InitiateCollection(ctx context.Context, req connector.CollectionRequest) (connector.CollectionResult, error) {
	body := map[string]any{
		"email":        req.CustomerEmail,
		"amount":       req.AmountMinor,
		"reference":    req.PaymentIntentID,
		"callback_url": req.CallbackURL,
		"currency":     req.Currency,
	}
	data, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL()+"/transaction/initialize",
		bytes.NewReader(data))
	if err != nil {
		return connector.CollectionResult{}, err
	}
	c.auth(httpReq)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return connector.CollectionResult{}, fmt.Errorf("paystack init: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var paystackResp struct {
		Status  bool   `json:"status"`
		Message string `json:"message"`
		Data    struct {
			AuthorizationURL string `json:"authorization_url"`
			AccessCode       string `json:"access_code"`
			Reference        string `json:"reference"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &paystackResp); err != nil {
		return connector.CollectionResult{}, fmt.Errorf("parse paystack response: %w", err)
	}

	if !paystackResp.Status {
		return connector.CollectionResult{},
			fmt.Errorf("paystack init failed: %s", paystackResp.Message)
	}

	return connector.CollectionResult{
		PSPReference: paystackResp.Data.Reference,
		NextAction: &connector.NextAction{
			Type: "redirect",
			URL:  paystackResp.Data.AuthorizationURL,
		},
		Status:      "requires_action",
		RawRequest:  data,
		RawResponse: respBody,
	}, nil
}

func (c *Connector) GetStatus(ctx context.Context, pspReference string) (connector.StatusResult, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET",
		c.baseURL()+"/transaction/verify/"+pspReference, nil)
	if err != nil {
		return connector.StatusResult{}, err
	}
	c.auth(httpReq)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return connector.StatusResult{}, fmt.Errorf("paystack verify: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var verifyResp struct {
		Status  bool   `json:"status"`
		Message string `json:"message"`
		Data    struct {
			Status          string `json:"status"`
			Amount          int64  `json:"amount"`
			Currency        string `json:"currency"`
			GatewayResponse string `json:"gateway_response"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &verifyResp); err != nil {
		return connector.StatusResult{}, fmt.Errorf("parse paystack verify: %w", err)
	}

	status := "succeeded"
	if !verifyResp.Status || verifyResp.Data.Status != "success" {
		status = "failed"
	}

	return connector.StatusResult{
		Supported:  true,
		Status:     status,
		AmountMinor: verifyResp.Data.Amount,
		Currency:   verifyResp.Data.Currency,
	}, nil
}

func (c *Connector) Refund(ctx context.Context, req connector.RefundRequest) (connector.RefundResult, error) {
	body := map[string]any{
		"transaction": req.PSPReference,
		"amount":      req.AmountMinor,
	}
	data, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL()+"/refund",
		bytes.NewReader(data))
	if err != nil {
		return connector.RefundResult{}, err
	}
	c.auth(httpReq)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return connector.RefundResult{}, fmt.Errorf("paystack refund: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var refundResp struct {
		Status  bool   `json:"status"`
		Message string `json:"message"`
		Data    struct {
			ID             int    `json:"id"`
			TransactionRef string `json:"transaction_reference"`
			Status         string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &refundResp); err != nil {
		return connector.RefundResult{}, fmt.Errorf("parse paystack refund: %w", err)
	}

	if !refundResp.Status {
		return connector.RefundResult{}, fmt.Errorf("paystack refund failed: %s", refundResp.Message)
	}

	return connector.RefundResult{
		PSPReference: fmt.Sprintf("ref_%d", refundResp.Data.ID),
		Status:       "accepted",
	}, nil
}

func (c *Connector) InitiatePayout(ctx context.Context, req connector.PayoutRequest) (connector.PayoutResult, error) {
	body := map[string]any{
		"source":   "balance",
		"reason":   "merchant payout",
		"amount":   req.AmountMinor,
		"recipient": req.BankAccountRef,
		"currency": req.Currency,
	}
	data, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL()+"/transfer",
		bytes.NewReader(data))
	if err != nil {
		return connector.PayoutResult{}, err
	}
	c.auth(httpReq)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return connector.PayoutResult{}, fmt.Errorf("paystack transfer: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var transferResp struct {
		Status  bool   `json:"status"`
		Message string `json:"message"`
		Data    struct {
			Reference    string `json:"reference"`
			Amount       int64  `json:"amount"`
			Currency     string `json:"currency"`
			Status       string `json:"status"`
			TransferCode string `json:"transfer_code"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &transferResp); err != nil {
		return connector.PayoutResult{}, fmt.Errorf("parse paystack transfer: %w", err)
	}

	if !transferResp.Status {
		return connector.PayoutResult{}, fmt.Errorf("paystack transfer failed: %s", transferResp.Message)
	}

	status := "accepted"
	if transferResp.Data.Status == "success" {
		status = "succeeded"
	}

	return connector.PayoutResult{
		PSPReference: transferResp.Data.TransferCode,
		Status:       status,
	}, nil
}

func (c *Connector) ParseWebhook(ctx context.Context, headers map[string]string, body []byte) (connector.WebhookEvent, error) {
	var event struct {
		Event string          `json:"event"`
		Data  json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		return connector.WebhookEvent{}, fmt.Errorf("parse paystack webhook: %w", err)
	}

	switch event.Event {
	case "charge.success":
		var charge struct {
			Reference string `json:"reference"`
			Amount    int64  `json:"amount"`
			Currency  string `json:"currency"`
			Status    string `json:"status"`
			ID        int    `json:"id"`
		}
		if err := json.Unmarshal(event.Data, &charge); err != nil {
			return connector.WebhookEvent{}, fmt.Errorf("parse charge.success data: %w", err)
		}

		return connector.WebhookEvent{
			PSP:          "paystack",
			EventType:    "charge.success",
			PSPReference: charge.Reference,
			DedupKey:     fmt.Sprintf("charge.success:%d", charge.ID),
			Status:       "succeeded",
			AmountMinor:  charge.Amount,
			Currency:     charge.Currency,
			RawPayload:   body,
		}, nil

	case "transfer.success":
		var transfer struct {
			TransferCode string `json:"transfer_code"`
			Reference    string `json:"reference"`
			Amount       int64  `json:"amount"`
			Currency     string `json:"currency"`
			Status       string `json:"status"`
			ID           int    `json:"id"`
		}
		if err := json.Unmarshal(event.Data, &transfer); err != nil {
			return connector.WebhookEvent{}, fmt.Errorf("parse transfer.success data: %w", err)
		}

		return connector.WebhookEvent{
			PSP:          "paystack",
			EventType:    "transfer.success",
			PSPReference: transfer.TransferCode,
			DedupKey:     fmt.Sprintf("transfer.success:%d", transfer.ID),
			Status:       "succeeded",
			AmountMinor:  transfer.Amount,
			Currency:     transfer.Currency,
			RawPayload:   body,
		}, nil

	case "transfer.failed":
		var transfer struct {
			TransferCode string `json:"transfer_code"`
			Reference    string `json:"reference"`
			Amount       int64  `json:"amount"`
			Currency     string `json:"currency"`
			ID           int    `json:"id"`
		}
		if err := json.Unmarshal(event.Data, &transfer); err != nil {
			return connector.WebhookEvent{}, fmt.Errorf("parse transfer.failed data: %w", err)
		}

		return connector.WebhookEvent{
			PSP:          "paystack",
			EventType:    "transfer.failed",
			PSPReference: transfer.TransferCode,
			DedupKey:     fmt.Sprintf("transfer.failed:%d", transfer.ID),
			Status:       "failed",
			AmountMinor:  transfer.Amount,
			Currency:     transfer.Currency,
			RawPayload:   body,
		}, nil

	case "refund.success":
		var refund struct {
			TransactionRef string `json:"transaction_reference"`
			Amount         int64  `json:"amount"`
			Currency       string `json:"currency"`
			Status         string `json:"status"`
			ID             int    `json:"id"`
		}
		if err := json.Unmarshal(event.Data, &refund); err != nil {
			return connector.WebhookEvent{}, fmt.Errorf("parse refund.success data: %w", err)
		}

		return connector.WebhookEvent{
			PSP:          "paystack",
			EventType:    "refund.success",
			PSPReference: refund.TransactionRef,
			DedupKey:     fmt.Sprintf("refund.success:%d", refund.ID),
			Status:       "succeeded",
			AmountMinor:  refund.Amount,
			Currency:     refund.Currency,
			RawPayload:   body,
		}, nil

	default:
		return connector.WebhookEvent{
			PSP:        "paystack",
			EventType:  event.Event,
			Status:     "unknown",
			RawPayload: body,
		}, nil
	}
}

func (c *Connector) auth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.config.SecretKey)
	req.Header.Set("Content-Type", "application/json")
}
