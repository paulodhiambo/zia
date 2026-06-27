package routing

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"
)

type RouteRequest struct {
	MerchantID  string
	Currency    string
	Country     string
	Method      string
	AmountMinor int64
}

type RouteDecision struct {
	Primary   string
	Fallbacks []string
	Reason    string
}

type Router interface {
	Route(ctx context.Context, req RouteRequest) (*RouteDecision, error)
}

type Rule struct {
	Priority   int       `json:"priority"`
	Conditions Condition `json:"conditions"`
	PrimaryPSP string    `json:"primary_psp"`
	Fallbacks  []string  `json:"fallbacks"`
	Enabled    bool      `json:"enabled"`
}

type Condition struct {
	Currency  string `json:"currency,omitempty"`
	Method    string `json:"method,omitempty"`
	Country   string `json:"country,omitempty"`
	Merchant  string `json:"merchant_id,omitempty"`
}

type Engine struct {
	mu       sync.RWMutex
	rules    []Rule
	cb       *CircuitBreaker
	logger   *zap.Logger
	excluded map[string]bool
}

func NewEngine(cb *CircuitBreaker, logger *zap.Logger) *Engine {
	e := &Engine{
		cb:       cb,
		logger:   logger,
		excluded: map[string]bool{"pesalink": true},
	}
	e.setDefaultRules()
	return e
}

func (e *Engine) setDefaultRules() {
	e.rules = []Rule{
		{
			Priority:   1,
			Conditions: Condition{Method: "mpesa_stk", Currency: "KES"},
			PrimaryPSP: "mpesa",
			Fallbacks:  []string{"kcb"},
			Enabled:    true,
		},
		{
			Priority:   2,
			Conditions: Condition{Method: "kcb_stk", Currency: "KES"},
			PrimaryPSP: "kcb",
			Fallbacks:  []string{"mpesa"},
			Enabled:    true,
		},
		{
			Priority:   3,
			Conditions: Condition{Method: "card", Currency: "KES"},
			PrimaryPSP: "paystack",
			Fallbacks:  []string{},
			Enabled:    true,
		},
		{
			Priority:   4,
			Conditions: Condition{Method: "card"},
			PrimaryPSP: "paystack",
			Fallbacks:  []string{},
			Enabled:    true,
		},
	}
}

func (e *Engine) Route(ctx context.Context, req RouteRequest) (*RouteDecision, error) {
	e.mu.RLock()
	rules := e.rules
	e.mu.RUnlock()

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if !matchCondition(rule.Conditions, req) {
			continue
		}
		if e.excluded[rule.PrimaryPSP] {
			continue
		}
		if !e.cb.IsAvailable(rule.PrimaryPSP) {
			e.logger.Warn("circuit breaker open for primary, trying fallbacks",
				zap.String("primary", rule.PrimaryPSP),
			)
			for _, fb := range rule.Fallbacks {
				if e.excluded[fb] {
					continue
				}
				if e.cb.IsAvailable(fb) {
					return &RouteDecision{
						Primary:   fb,
						Fallbacks: nil,
						Reason:    fmt.Sprintf("circuit breaker open for %s, fell back to %s", rule.PrimaryPSP, fb),
					}, nil
				}
			}
			return nil, fmt.Errorf("no available connector for method=%s currency=%s", req.Method, req.Currency)
		}

		var availableFallbacks []string
		for _, fb := range rule.Fallbacks {
			if !e.excluded[fb] && e.cb.IsAvailable(fb) {
				availableFallbacks = append(availableFallbacks, fb)
			}
		}

		return &RouteDecision{
			Primary:   rule.PrimaryPSP,
			Fallbacks: availableFallbacks,
			Reason:    fmt.Sprintf("rule priority %d matched", rule.Priority),
		}, nil
	}

	return nil, fmt.Errorf("no routing rule matched for method=%s currency=%s country=%s", req.Method, req.Currency, req.Country)
}

func matchCondition(c Condition, req RouteRequest) bool {
	if c.Currency != "" && c.Currency != req.Currency {
		return false
	}
	if c.Method != "" && c.Method != req.Method {
		return false
	}
	if c.Country != "" && c.Country != req.Country {
		return false
	}
	if c.Merchant != "" && c.Merchant != req.MerchantID {
		return false
	}
	return true
}
