package mpesa

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"
)

type TokenManager struct {
	cfg       Config
	mu        sync.Mutex
	token     string
	expiresAt time.Time
	logger    *zap.Logger
}

func NewTokenManager(cfg Config, logger *zap.Logger) *TokenManager {
	return &TokenManager{cfg: cfg, logger: logger}
}

func (t *TokenManager) Token(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if time.Now().Before(t.expiresAt.Add(-60 * time.Second)) {
		return t.token, nil
	}

	token, expiresIn, err := t.fetchToken(ctx)
	if err != nil {
		return "", fmt.Errorf("fetch mpesa token: %w", err)
	}

	t.token = token
	t.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	return t.token, nil
}

func (t *TokenManager) fetchToken(ctx context.Context) (string, int, error) {
	baseURL := "https://api.safaricom.co.ke"
	if t.cfg.Sandbox {
		baseURL = "https://sandbox.safaricom.co.ke"
	}

	auth := base64.StdEncoding.EncodeToString(
		[]byte(t.cfg.ConsumerKey + ":" + t.cfg.ConsumerSecret),
	)

	req, err := http.NewRequestWithContext(ctx, "GET",
		baseURL+"/oauth/v1/generate?grant_type=client_credentials", nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "Basic "+auth)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	t.logger.Info("mpesa auth response",
		zap.Int("status", resp.StatusCode),
		zap.ByteString("body", body),
	)

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("mpesa auth failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   string `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.logger.Error("mpesa auth json parse failed",
			zap.Int("status", resp.StatusCode),
			zap.ByteString("body", body),
			zap.Error(err),
		)
		return "", 0, fmt.Errorf("parse auth response: %w body=%s", err, string(body))
	}

	expiresIn, err := strconv.Atoi(result.ExpiresIn)
	if err != nil {
		t.logger.Warn("mpesa auth non-numeric expires_in",
			zap.String("raw", result.ExpiresIn),
		)
		expiresIn = 3599
	}

	return result.AccessToken, expiresIn, nil
}
