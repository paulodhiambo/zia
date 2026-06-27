package routing

import (
	"sync"
	"time"
)

type BreakerConfig struct {
	Threshold int
	Cooldown  time.Duration
}

type BreakerState struct {
	Failures    int
	LastFailure time.Time
	Open        bool
	Cooldown    time.Duration
}

type CircuitBreaker struct {
	mu         sync.Mutex
	states     map[string]*BreakerState
	thresholds map[string]BreakerConfig
}

func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{
		states:     make(map[string]*BreakerState),
		thresholds: make(map[string]BreakerConfig),
	}
}

func (cb *CircuitBreaker) SetConfig(psp string, threshold int, cooldown time.Duration) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.thresholds[psp] = BreakerConfig{Threshold: threshold, Cooldown: cooldown}
}

func (cb *CircuitBreaker) RecordFailure(psp string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	state, ok := cb.states[psp]
	if !ok {
		cooldown := 60 * time.Second
		if cfg, ok := cb.thresholds[psp]; ok {
			cooldown = cfg.Cooldown
		}
		cb.states[psp] = &BreakerState{
			Failures: 1,
			Cooldown: cooldown,
		}
		return
	}

	state.Failures++
	state.LastFailure = time.Now()

	threshold := 5
	if cfg, ok := cb.thresholds[psp]; ok {
		threshold = cfg.Threshold
	}

	if state.Failures >= threshold && !state.Open {
		state.Open = true
	}
}

func (cb *CircuitBreaker) RecordSuccess(psp string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	state, ok := cb.states[psp]
	if !ok {
		return
	}
	state.Failures = 0
	state.Open = false
}

func (cb *CircuitBreaker) IsAvailable(psp string) bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	state, ok := cb.states[psp]
	if !ok {
		return true
	}

	if !state.Open {
		return true
	}

	if time.Since(state.LastFailure) > state.Cooldown {
		state.Open = false
		state.Failures = 0
		return true
	}

	return false
}

func (cb *CircuitBreaker) State(psp string) BreakerState {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if state, ok := cb.states[psp]; ok {
		return *state
	}
	return BreakerState{}
}
