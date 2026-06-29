package risk

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Request struct {
	MerchantID    string
	Amount   int64
	Currency      string
	Method        string
	CustomerPhone string
	CustomerEmail string
}

type Engine struct {
	mu       sync.Mutex
	velocities map[string][]time.Time
	maxAttempts int
	window      time.Duration
	maxAmount   int64
}

func NewEngine() *Engine {
	return &Engine{
		velocities:  make(map[string][]time.Time),
		maxAttempts: 10,
		window:      1 * time.Minute,
		maxAmount:   1_000_000_00,
	}
}

func (e *Engine) Evaluate(_ context.Context, req Request) error {
	if req.Amount <= 0 {
		return fmt.Errorf("amount must be positive")
	}

	if req.Amount > e.maxAmount {
		return fmt.Errorf("amount exceeds maximum: %d > %d", req.Amount, e.maxAmount)
	}

	if req.CustomerPhone != "" {
		if err := e.checkVelocity(fmt.Sprintf("phone:%s", req.CustomerPhone)); err != nil {
			return err
		}
	}

	if req.CustomerEmail != "" {
		if err := e.checkVelocity(fmt.Sprintf("email:%s", req.CustomerEmail)); err != nil {
			return err
		}
	}

	return nil
}

func (e *Engine) checkVelocity(key string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-e.window)

	times := e.velocities[key]
	var recent []time.Time
	for _, t := range times {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	e.velocities[key] = recent

	if len(recent) >= e.maxAttempts {
		return fmt.Errorf("rate limit exceeded for %s: %d attempts in %v", key, len(recent), e.window)
	}

	e.velocities[key] = append(e.velocities[key], now)
	return nil
}
