package mpesa

import (
	"bytes"
	"context"
	"encoding/base64"
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
	ShortCode      string
	PassKey        string
	CallbackBase   string
	Sandbox        bool

	B2CInitiatorName string
	B2CSecurityCred  string
	AllowedIPs       []string
}

type Connector struct {
	config Config
	http   *http.Client
	auth   *TokenManager
}

func New(cfg Config) *Connector {
	return &Connector{
		config: cfg,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
		auth: NewTokenManager(cfg),
	}
}

func (c *Connector) Name() string { return "mpesa" }

func (c *Connector) Capabilities() connector.Capabilities {
	return connector.Capabilities{
		SupportsCollection:  true,
		SupportsPayout:      true,
		SupportsRefund:      true,
		SupportedCurrencies: []string{"KES"},
		SupportedCountries:  []string{"KE"},
		ConfirmationStyle:   "webhook_only",
	}
}

func (c *Connector) baseURL() string {
	if c.config.Sandbox {
		return "https://sandbox.safaricom.co.ke"
	}
	return "https://api.safaricom.co.ke"
}

type stkPushRequest struct {
	BusinessShortCode string `json:"BusinessShortCode"`
	Password          string `json:"Password"`
	Timestamp         string `json:"Timestamp"`
	TransactionType   string `json:"TransactionType"`
	Amount            int64  `json:"Amount"`
	PartyA            string `json:"PartyA"`
	PartyB            string `json:"PartyB"`
	PhoneNumber       string `json:"PhoneNumber"`
	CallBackURL       string `json:"CallBackURL"`
	AccountReference  string `json:"AccountReference"`
	TransactionDesc   string `json:"TransactionDesc"`
}

type stkPushResponse struct {
	MerchantRequestID   string `json:"MerchantRequestID"`
	CheckoutRequestID   string `json:"CheckoutRequestID"`
	ResponseCode        string `json:"ResponseCode"`
	ResponseDescription string `json:"ResponseDescription"`
	CustomerMessage     string `json:"CustomerMessage"`
}

func (c *Connector) InitiateCollection(ctx context.Context, req connector.CollectionRequest) (connector.CollectionResult, error) {
	token, err := c.auth.Token(ctx)
	if err != nil {
		return connector.CollectionResult{}, fmt.Errorf("auth: %w", err)
	}

	timestamp := time.Now().UTC().Format("20060102150405")
	password := c.stkPassword(timestamp)

	callbackURL := c.config.CallbackBase + "/v1/webhooks/mpesa"
	if req.CallbackURL != "" {
		callbackURL = req.CallbackURL
	}

	body := stkPushRequest{
		BusinessShortCode: c.config.ShortCode,
		Password:          password,
		Timestamp:         timestamp,
		TransactionType:   "CustomerPayBillOnline",
		Amount:            req.AmountMinor,
		PartyA:            req.CustomerPhone,
		PartyB:            c.config.ShortCode,
		PhoneNumber:       req.CustomerPhone,
		CallBackURL:       callbackURL,
		AccountReference:  truncate(req.PaymentIntentID, 12),
		TransactionDesc:   "Payment",
	}

	data, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL()+"/mpesa/stkpush/v1/processrequest",
		bytes.NewReader(data))
	if err != nil {
		return connector.CollectionResult{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return connector.CollectionResult{}, fmt.Errorf("stk push request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return connector.CollectionResult{},
			fmt.Errorf("stk push failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var stkResp stkPushResponse
	if err := json.Unmarshal(respBody, &stkResp); err != nil {
		return connector.CollectionResult{}, fmt.Errorf("parse stk response: %w", err)
	}

	if stkResp.ResponseCode != "0" {
		return connector.CollectionResult{},
			fmt.Errorf("stk push rejected: code=%s desc=%s", stkResp.ResponseCode, stkResp.ResponseDescription)
	}

	return connector.CollectionResult{
		PSPReference: stkResp.CheckoutRequestID,
		Status:       "requires_action",
	}, nil
}

func (c *Connector) GetStatus(ctx context.Context, pspReference string) (connector.StatusResult, error) {
	token, err := c.auth.Token(ctx)
	if err != nil {
		return connector.StatusResult{}, fmt.Errorf("auth: %w", err)
	}

	timestamp := time.Now().UTC().Format("20060102150405")
	password := c.stkPassword(timestamp)

	body := map[string]string{
		"BusinessShortCode": c.config.ShortCode,
		"Password":          password,
		"Timestamp":         timestamp,
		"CheckoutRequestID": pspReference,
	}

	data, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL()+"/mpesa/stkpushquery/v1/query",
		bytes.NewReader(data))
	if err != nil {
		return connector.StatusResult{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return connector.StatusResult{}, fmt.Errorf("query request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var queryResp struct {
		ResponseCode        string `json:"ResponseCode"`
		ResultCode          string `json:"ResultCode"`
		ResultDesc          string `json:"ResultDesc"`
	}
	if err := json.Unmarshal(respBody, &queryResp); err != nil {
		return connector.StatusResult{}, fmt.Errorf("parse query response: %w", err)
	}

	status := "failed"
	if queryResp.ResultCode == "0" {
		status = "succeeded"
	} else if queryResp.ResultCode == "1037" {
		status = "pending"
	}

	return connector.StatusResult{
		Supported:   true,
		Status:      status,
		AmountMinor: 0,
	}, nil
}

func (c *Connector) Refund(ctx context.Context, req connector.RefundRequest) (connector.RefundResult, error) {
	token, err := c.auth.Token(ctx)
	if err != nil {
		return connector.RefundResult{}, fmt.Errorf("auth: %w", err)
	}

	body := map[string]any{
		"InitiatorName":      c.config.B2CInitiatorName,
		"SecurityCredential": c.config.B2CSecurityCred,
		"CommandID":          "BusinessPayment",
		"Amount":             req.AmountMinor,
		"PartyA":             c.config.ShortCode,
		"PartyB":             req.PSPReference,
		"Remarks":            "Refund",
		"Occasion":           truncate(req.PaymentIntentID, 12),
	}

	data, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL()+"/mpesa/b2c/v3/paymentrequest",
		bytes.NewReader(data))
	if err != nil {
		return connector.RefundResult{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return connector.RefundResult{}, fmt.Errorf("b2c request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return connector.RefundResult{}, fmt.Errorf("b2c failed: status=%d", resp.StatusCode)
	}

	var b2cResp struct {
		ConversationID           string `json:"ConversationID"`
		OriginatorConversationID string `json:"OriginatorConversationID"`
		ResponseCode             string `json:"ResponseCode"`
	}
	respBody, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(respBody, &b2cResp); err != nil {
		return connector.RefundResult{}, fmt.Errorf("parse b2c response: %w", err)
	}

	if b2cResp.ResponseCode != "0" {
		return connector.RefundResult{}, fmt.Errorf("b2c rejected: code=%s", b2cResp.ResponseCode)
	}

	return connector.RefundResult{
		PSPReference: b2cResp.OriginatorConversationID,
		Status:       "pending",
	}, nil
}

func (c *Connector) InitiatePayout(ctx context.Context, req connector.PayoutRequest) (connector.PayoutResult, error) {
	return connector.PayoutResult{}, fmt.Errorf("mpesa: payout not supported via collection connector")
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
		return connector.WebhookEvent{}, fmt.Errorf("parse mpesa callback: %w", err)
	}

	sc := callback.Body.StkCallback
	dedupKey := sc.MerchantRequestID + ":" + sc.CheckoutRequestID

	var pspReference string
	var amountMinor int64
	if sc.CallbackMetadata != nil {
		for _, item := range sc.CallbackMetadata.Item {
			switch item.Name {
			case "MpesaReceiptNumber":
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
	case 2001:
		status = "failed"
	}

	if pspReference == "" {
		pspReference = sc.CheckoutRequestID
	}

	return connector.WebhookEvent{
		PSP:          "mpesa",
		EventType:    "stk_callback",
		PSPReference: pspReference,
		DedupKey:     dedupKey,
		Status:       status,
		AmountMinor:  amountMinor,
		Currency:     "KES",
		RawPayload:   body,
	}, nil
}

func (c *Connector) stkPassword(timestamp string) string {
	raw := c.config.ShortCode + c.config.PassKey + timestamp
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

