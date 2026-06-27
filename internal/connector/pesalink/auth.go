package pesalink

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type TokenManager struct {
	cfg       Config
	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

func NewTokenManager(cfg Config) *TokenManager {
	return &TokenManager{cfg: cfg}
}

func (t *TokenManager) Token(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if time.Now().Before(t.expiresAt.Add(-60 * time.Second)) {
		return t.token, nil
	}

	token, expiresIn, err := t.fetchToken(ctx)
	if err != nil {
		return "", fmt.Errorf("fetch pesalink token: %w", err)
	}

	t.token = token
	t.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	return t.token, nil
}

func (t *TokenManager) fetchToken(ctx context.Context) (string, int, error) {
	baseURL := "https://api.pesalink.com"
	if t.cfg.Sandbox {
		baseURL = "https://sandbox.api.pesalink.com"
	}

	auth := base64.StdEncoding.EncodeToString(
		[]byte(t.cfg.APIKey + ":" + t.cfg.APISecret),
	)

	req, err := http.NewRequestWithContext(ctx, "POST",
		baseURL+"/v1/auth/token", nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("pesalink auth failed: status=%d", resp.StatusCode)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", 0, err
	}

	return result.AccessToken, result.ExpiresIn, nil
}
