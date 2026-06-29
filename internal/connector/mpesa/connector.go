package mpesa

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"zia/internal/connector"

	"go.uber.org/zap"
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
	logger *zap.Logger
}

func New(cfg Config, logger *zap.Logger) *Connector {
	return &Connector{
		config: cfg,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
		auth:   NewTokenManager(cfg, logger),
		logger: logger,
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
	c.logger.Info("mpesa InitiateCollection",
		zap.String("pi_id", req.PaymentIntentID),
		zap.Int64("amount", req.Amount),
		zap.String("currency", req.Currency),
		zap.String("phone", req.CustomerPhone),
	)

	token, err := c.auth.Token(ctx)
	if err != nil {
		c.logger.Error("mpesa auth token failed",
			zap.String("pi_id", req.PaymentIntentID),
			zap.Error(err),
		)
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
		Amount:            req.Amount, // Daraja expects whole KES
		PartyA:            req.CustomerPhone,
		PartyB:            c.config.ShortCode,
		PhoneNumber:       req.CustomerPhone,
		CallBackURL:       callbackURL,
		AccountReference:  truncate(req.PaymentIntentID, 12),
		TransactionDesc:   "Payment",
	}

	data, _ := json.Marshal(body)

	sanitized := body
	sanitized.Password = "****"
	sanitizedData, _ := json.Marshal(sanitized)

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL()+"/mpesa/stkpush/v1/processrequest",
		bytes.NewReader(data))
	if err != nil {
		return connector.CollectionResult{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	c.logger.Info("mpesa stk push request",
		zap.String("url", c.baseURL()+"/mpesa/stkpush/v1/processrequest"),
		zap.String("short_code", c.config.ShortCode),
		zap.Int64("amount", req.Amount),
		zap.String("phone", req.CustomerPhone),
	)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		c.logger.Error("mpesa stk push http failed",
			zap.String("pi_id", req.PaymentIntentID),
			zap.Error(err),
		)
		return connector.CollectionResult{}, fmt.Errorf("stk push request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	c.logger.Info("mpesa stk push response",
		zap.Int("status", resp.StatusCode),
		zap.ByteString("body", respBody),
	)

	if resp.StatusCode != http.StatusOK {
		return connector.CollectionResult{},
			fmt.Errorf("stk push failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var stkResp stkPushResponse
	if err := json.Unmarshal(respBody, &stkResp); err != nil {
		c.logger.Error("mpesa stk push json parse failed",
			zap.Int("status", resp.StatusCode),
			zap.ByteString("body", respBody),
			zap.Error(err),
		)
		return connector.CollectionResult{}, fmt.Errorf("parse stk response: %w", err)
	}

	c.logger.Info("mpesa stk push parsed",
		zap.String("merchant_request_id", stkResp.MerchantRequestID),
		zap.String("checkout_request_id", stkResp.CheckoutRequestID),
		zap.String("response_code", stkResp.ResponseCode),
		zap.String("response_description", stkResp.ResponseDescription),
	)

	if stkResp.ResponseCode != "0" {
		c.logger.Error("mpesa stk push rejected",
			zap.String("response_code", stkResp.ResponseCode),
			zap.String("response_description", stkResp.ResponseDescription),
		)
		return connector.CollectionResult{},
			fmt.Errorf("stk push rejected: code=%s desc=%s", stkResp.ResponseCode, stkResp.ResponseDescription)
	}

	return connector.CollectionResult{
		PSPReference: stkResp.CheckoutRequestID,
		Status:       "requires_action",
		RawRequest:   sanitizedData,
		RawResponse:  respBody,
	}, nil
}

func (c *Connector) GetStatus(ctx context.Context, pspReference string) (connector.StatusResult, error) {
	c.logger.Info("mpesa GetStatus",
		zap.String("psp_reference", pspReference),
	)

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

	c.logger.Info("mpesa query request",
		zap.String("url", c.baseURL()+"/mpesa/stkpushquery/v1/query"),
		zap.String("psp_reference", pspReference),
	)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		c.logger.Error("mpesa query http failed",
			zap.String("psp_reference", pspReference),
			zap.Error(err),
		)
		return connector.StatusResult{}, fmt.Errorf("query request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	c.logger.Info("mpesa query response",
		zap.Int("status", resp.StatusCode),
		zap.ByteString("body", respBody),
	)

	var queryResp struct {
		ResponseCode        string `json:"ResponseCode"`
		ResultCode          string `json:"ResultCode"`
		ResultDesc          string `json:"ResultDesc"`
	}
	if err := json.Unmarshal(respBody, &queryResp); err != nil {
		c.logger.Error("mpesa query json parse failed",
			zap.Int("status", resp.StatusCode),
			zap.ByteString("body", respBody),
			zap.Error(err),
		)
		return connector.StatusResult{}, fmt.Errorf("parse query response: %w", err)
	}

	c.logger.Info("mpesa query parsed",
		zap.String("response_code", queryResp.ResponseCode),
		zap.String("result_code", queryResp.ResultCode),
		zap.String("result_desc", queryResp.ResultDesc),
	)

	status := "failed"
	if queryResp.ResultCode == "0" {
		status = "succeeded"
	} else if queryResp.ResultCode == "1037" {
		status = "pending"
	}

	return connector.StatusResult{
		Supported:   true,
		Status:      status,
		Amount: 0,
	}, nil
}

func (c *Connector) Refund(ctx context.Context, req connector.RefundRequest) (connector.RefundResult, error) {
	c.logger.Info("mpesa Refund",
		zap.String("pi_id", req.PaymentIntentID),
		zap.Int64("amount", req.Amount),
		zap.String("psp_reference", req.PSPReference),
	)

	token, err := c.auth.Token(ctx)
	if err != nil {
		return connector.RefundResult{}, fmt.Errorf("auth: %w", err)
	}

	body := map[string]any{
		"InitiatorName":      c.config.B2CInitiatorName,
		"SecurityCredential": c.config.B2CSecurityCred,
		"CommandID":          "BusinessPayment",
		"Amount":             req.Amount,
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

	c.logger.Info("mpesa b2c request",
		zap.String("url", c.baseURL()+"/mpesa/b2c/v3/paymentrequest"),
		zap.Int64("amount", req.Amount),
	)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		c.logger.Error("mpesa b2c http failed",
			zap.String("pi_id", req.PaymentIntentID),
			zap.Error(err),
		)
		return connector.RefundResult{}, fmt.Errorf("b2c request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	c.logger.Info("mpesa b2c response",
		zap.Int("status", resp.StatusCode),
		zap.ByteString("body", respBody),
	)

	if resp.StatusCode != http.StatusOK {
		return connector.RefundResult{}, fmt.Errorf("b2c failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var b2cResp struct {
		ConversationID           string `json:"ConversationID"`
		OriginatorConversationID string `json:"OriginatorConversationID"`
		ResponseCode             string `json:"ResponseCode"`
	}
	if err := json.Unmarshal(respBody, &b2cResp); err != nil {
		c.logger.Error("mpesa b2c json parse failed",
			zap.Int("status", resp.StatusCode),
			zap.ByteString("body", respBody),
			zap.Error(err),
		)
		return connector.RefundResult{}, fmt.Errorf("parse b2c response: %w", err)
	}

	c.logger.Info("mpesa b2c parsed",
		zap.String("response_code", b2cResp.ResponseCode),
		zap.String("conversation_id", b2cResp.ConversationID),
	)

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
	c.logger.Info("mpesa ParseWebhook",
		zap.Int("body_len", len(body)),
		zap.ByteString("body_preview", truncateBytes(body, 500)),
	)

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
						Value interface{} `json:"Value"`
					} `json:"Item"`
				} `json:"CallbackMetadata"`
			} `json:"stkCallback"`
		} `json:"Body"`
	}

	if err := json.Unmarshal(body, &callback); err != nil {
		c.logger.Error("mpesa webhook json parse failed",
			zap.ByteString("body", body),
			zap.Error(err),
		)
		return connector.WebhookEvent{}, fmt.Errorf("parse mpesa callback: %w", err)
	}

	sc := callback.Body.StkCallback
	dedupKey := sc.MerchantRequestID + ":" + sc.CheckoutRequestID

	c.logger.Info("mpesa webhook parsed",
		zap.String("merchant_request_id", sc.MerchantRequestID),
		zap.String("checkout_request_id", sc.CheckoutRequestID),
		zap.Int("result_code", sc.ResultCode),
		zap.String("result_desc", sc.ResultDesc),
	)

	var pspTransactionID string
	var amount int64
	if sc.CallbackMetadata != nil {
		for _, item := range sc.CallbackMetadata.Item {
			switch item.Name {
			case "MpesaReceiptNumber":
				pspTransactionID = valueToString(item.Value)
			case "Amount":
				amount = valueToInt64(item.Value)
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

	c.logger.Info("mpesa webhook mapped",
		zap.String("checkout_request_id", sc.CheckoutRequestID),
		zap.String("receipt_number", pspTransactionID),
		zap.String("status", status),
		zap.Int64("amount", amount),
	)

	return connector.WebhookEvent{
		PSP:              "mpesa",
		EventType:        "stk_callback",
		PSPReference:     sc.CheckoutRequestID,
		PSPTransactionID: pspTransactionID,
		DedupKey:         dedupKey,
		Status:           status,
		Amount:      amount,
		Currency:         "KES",
		RawPayload:       body,
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

func truncateBytes(b []byte, maxLen int) []byte {
	if len(b) <= maxLen {
		return b
	}
	return b[:maxLen]
}

func valueToString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case json.Number:
		return val.String()
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

func valueToInt64(v interface{}) int64 {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return int64(val)
	case json.Number:
		n, _ := val.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(val, 10, 64)
		return n
	default:
		return 0
	}
}

