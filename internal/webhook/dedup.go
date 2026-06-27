package webhook

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type DedupStore struct {
	rdb redis.UniversalClient
	ttl time.Duration
}

func NewDedupStore(rdb redis.UniversalClient) *DedupStore {
	return &DedupStore{
		rdb: rdb,
		ttl: 7 * 24 * time.Hour,
	}
}

func (s *DedupStore) Check(ctx context.Context, dedupKey string) (bool, error) {
	redisKey := fmt.Sprintf("webhook_dedup:%s", dedupKey)
	_, err := s.rdb.Get(ctx, redisKey).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *DedupStore) Mark(ctx context.Context, dedupKey string) error {
	redisKey := fmt.Sprintf("webhook_dedup:%s", dedupKey)
	return s.rdb.Set(ctx, redisKey, "1", s.ttl).Err()
}
