package idempotency

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Store struct {
	rdb redis.UniversalClient
	ttl time.Duration
}

type entry struct {
	PaymentIntentID string `json:"pi_id"`
}

func NewStore(rdb redis.UniversalClient) *Store {
	return &Store{
		rdb: rdb,
		ttl: 24 * time.Hour,
	}
}

func (s *Store) Check(ctx context.Context, merchantID, key string) (string, error) {
	redisKey := fmt.Sprintf("idempotency:%s:%s", merchantID, key)
	data, err := s.rdb.Get(ctx, redisKey).Bytes()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("redis get: %w", err)
	}

	var e entry
	if err := json.Unmarshal(data, &e); err != nil {
		return "", fmt.Errorf("unmarshal idempotency entry: %w", err)
	}
	return e.PaymentIntentID, nil
}

func (s *Store) Store(ctx context.Context, merchantID, key, paymentIntentID string) error {
	redisKey := fmt.Sprintf("idempotency:%s:%s", merchantID, key)
	e := entry{PaymentIntentID: paymentIntentID}
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal idempotency entry: %w", err)
	}
	return s.rdb.Set(ctx, redisKey, data, s.ttl).Err()
}
