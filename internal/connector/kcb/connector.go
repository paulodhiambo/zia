package kcb

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
	ConsumerKey    string
	ConsumerSecret string
	OrgShortCode   string
	CallbackURL    string
	AllowedIPs     []string
	Sandbox        bool
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

func (c *Connector) Name() string { return "kcb" }

func (c *Connector) Capabilities() connector.Capabilities {
	return connector.Capabilities{
		SupportsCollection:  true,
		SupportsPayout:      false,
		SupportsRefund:      false,
		SupportedCurrencies: []string{"KES"},
		SupportedCountries:  []string{"KE"},
		ConfirmationStyle:   "webhook_only",
	}
}

func (c *Connector) baseURL() string {
	if c.config.Sandbox {
		return "https://sandbox.buni.kcbgroup.com"
	}
	return "https://api.buni.kcbgroup.com"
}

func (c *Connector) InitiateCollection(ctx context.Context, req connector.CollectionRequest) (connector.CollectionResult, error) {
	token, err := c.auth.Token(ctx)
	if err != nil {
		return connector.CollectionResult{}, fmt.Errorf("auth: %w", err)
	}

	callbackURL := c.config.CallbackURL
	if req.CallbackURL != "" {
		callbackURL = req.CallbackURL
	}

	body := map[string]any{
		"BusinessShortCode": c.config.OrgShortCode,
		"Amount":            req.AmountMinor,
		"PartyA":            req.CustomerPhone,
		"PartyB":            c.config.OrgShortCode,
		"PhoneNumber":       req.CustomerPhone,
		"CallBackURL":       callbackURL,
		"AccountReference":  truncate(req.PaymentIntentID, 12),
		"TransactionDesc":   "Payment",
	}

	data, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL()+"/api/v1/mpesa/express/stk",
		bytes.NewReader(data))
	if err != nil {
		return connector.CollectionResult{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return connector.CollectionResult{}, fmt.Errorf("kcb stk request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return connector.CollectionResult{},
			fmt.Errorf("kcb stk failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var kcbResp struct {
		ResponseCode        string `json:"ResponseCode"`
		ResponseDescription string `json:"ResponseDescription"`
		CustomerMessage     string `json:"CustomerMessage"`
		CheckoutRequestID   string `json:"CheckoutRequestID"`
		MerchantRequestID   string `json:"MerchantRequestID"`
	}
	if err := json.Unmarshal(respBody, &kcbResp); err != nil {
		return connector.CollectionResult{}, fmt.Errorf("parse kcb response: %w", err)
	}

	if kcbResp.ResponseCode != "0" {
		return connector.CollectionResult{},
			fmt.Errorf("kcb stk rejected: code=%s desc=%s", kcbResp.ResponseCode, kcbResp.ResponseDescription)
	}

	return connector.CollectionResult{
		PSPReference: kcbResp.CheckoutRequestID,
		Status:       "requires_action",
	}, nil
}

func (c *Connector) GetStatus(ctx context.Context, pspReference string) (connector.StatusResult, error) {
	return connector.StatusResult{Supported: false}, nil
}

func (c *Connector) Refund(ctx context.Context, req connector.RefundRequest) (connector.RefundResult, error) {
	return connector.RefundResult{}, fmt.Errorf("kcb: refund not supported in V1")
}

func (c *Connector) InitiatePayout(ctx context.Context, req connector.PayoutRequest) (connector.PayoutResult, error) {
	return connector.PayoutResult{}, fmt.Errorf("kcb: payout not supported in V1")
}

func (c *Connector) ParseWebhook(ctx context.Context, headers map[string]string, body []byte) (connector.WebhookEvent, error) {
	var callback struct {
		Body struct {
			StkCallback struct {
				MerchantRequestID string `json:"MerchantRequestID"`
				CheckoutRequestID string `json:"CheckoutRequestID"`
				ResultCode        int    `json:"ResultCode"`
				ResultDesc        string `json:"ResultDesc"`
				CallbackMetadata  *struct {
					Item []struct {
						Name  string      `json:"Name"`
						Value json.Number `json:"Value"`
					} `json:"Item"`
				} `json:"CallbackMetadata"`
			} `json:"stkCallback"`
		} `json:"Body"`
	}

	if err := json.Unmarshal(body, &callback); err != nil {
		return connector.WebhookEvent{}, fmt.Errorf("parse kcb callback: %w", err)
	}

	sc := callback.Body.StkCallback
	dedupKey := sc.MerchantRequestID + ":" + sc.CheckoutRequestID

	var pspReference string
	var amountMinor int64
	if sc.CallbackMetadata != nil {
		for _, item := range sc.CallbackMetadata.Item {
			switch item.Name {
			case "TransactionID":
				pspReference = item.Value.String()
			case "Amount":
				if v, err := item.Value.Int64(); err == nil {
					amountMinor = v
				}
			}
		}
	}

	status := "failed"
	switch sc.ResultCode {
	case 0:
		status = "succeeded"
	case 1032:
		status = "failed"
	case 1037:
		status = "failed"
	}

	if pspReference == "" {
		pspReference = sc.CheckoutRequestID
	}

	return connector.WebhookEvent{
		PSP:          "kcb",
		EventType:    "stk_callback",
		PSPReference: pspReference,
		DedupKey:     dedupKey,
		Status:       status,
		AmountMinor:  amountMinor,
		Currency:     "KES",
		RawPayload:   body,
	}, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
