package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/vietddude/watcher/internal/core/domain"
)

// FailedBlockRepo implements FailedBlockRepository using Redis.
type FailedBlockRepo struct {
	rdb          *redis.Client
	internalCode string
}

// NewFailedBlockRepo creates a new Redis-backed failed block repository.
func NewFailedBlockRepo(client *Client, internalCode string) *FailedBlockRepo {
	return &FailedBlockRepo{
		rdb:          client.rdb,
		internalCode: internalCode,
	}
}

// Key helpers
func (r *FailedBlockRepo) queueKey() string {
	return fmt.Sprintf("failed_blocks:%s", r.internalCode)
}

func (r *FailedBlockRepo) blockKey(id string) string {
	return fmt.Sprintf("failed_block:%s:%s", r.internalCode, id)
}

// Add adds a failed block to the queue.
func (r *FailedBlockRepo) Add(ctx context.Context, fb *domain.FailedBlock) error {
	// Serialize the failed block
	data, err := json.Marshal(fb)
	if err != nil {
		return fmt.Errorf("failed to marshal failed block: %w", err)
	}

	// Store the data
	if err := r.rdb.Set(ctx, r.blockKey(fb.ID), data, 24*time.Hour).Err(); err != nil {
		return fmt.Errorf("failed to set failed block: %w", err)
	}

	// Add to sorted set (score = retry count for priority, lower = retry first)
	if err := r.rdb.ZAdd(ctx, r.queueKey(), redis.Z{
		Score:  float64(fb.RetryCount),
		Member: fb.ID,
	}).Err(); err != nil {
		return fmt.Errorf("failed to add to queue: %w", err)
	}

	return nil
}

// GetNext retrieves the next failed block to retry.
func (r *FailedBlockRepo) GetNext(ctx context.Context, chainID string) (*domain.FailedBlock, error) {
	// Get the first member (lowest retry count)
	results, err := r.rdb.ZRange(ctx, r.queueKey(), 0, 0).Result()
	if err != nil {
		return nil, fmt.Errorf("zrange failed: %w", err)
	}

	if len(results) == 0 {
		return nil, nil
	}

	id := results[0]

	// Get the data
	data, err := r.rdb.Get(ctx, r.blockKey(id)).Bytes()
	if err == redis.Nil {
		// Data expired but ID still in queue, remove it
		r.rdb.ZRem(ctx, r.queueKey(), id)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get failed block: %w", err)
	}

	var fb domain.FailedBlock
	if err := json.Unmarshal(data, &fb); err != nil {
		return nil, fmt.Errorf("failed to unmarshal failed block: %w", err)
	}

	return &fb, nil
}

// IncrementRetry increments retry count and updates last attempt.
func (r *FailedBlockRepo) IncrementRetry(ctx context.Context, id string) error {
	// Get current data
	data, err := r.rdb.Get(ctx, r.blockKey(id)).Bytes()
	if err != nil {
		return fmt.Errorf("failed to get failed block: %w", err)
	}

	var fb domain.FailedBlock
	if err := json.Unmarshal(data, &fb); err != nil {
		return fmt.Errorf("failed to unmarshal failed block: %w", err)
	}

	// Update
	fb.RetryCount++
	fb.LastAttempt = time.Now()

	// Save
	newData, err := json.Marshal(fb)
	if err != nil {
		return fmt.Errorf("failed to marshal failed block: %w", err)
	}

	if err := r.rdb.Set(ctx, r.blockKey(id), newData, 24*time.Hour).Err(); err != nil {
		return fmt.Errorf("failed to set failed block: %w", err)
	}

	// Update score in queue (higher retry count = lower priority)
	if err := r.rdb.ZAdd(ctx, r.queueKey(), redis.Z{
		Score:  float64(fb.RetryCount),
		Member: id,
	}).Err(); err != nil {
		return fmt.Errorf("failed to update queue: %w", err)
	}

	return nil
}

// MarkResolved removes a failed block (successfully retried).
func (r *FailedBlockRepo) MarkResolved(ctx context.Context, id string) error {
	// Remove from queue
	if err := r.rdb.ZRem(ctx, r.queueKey(), id).Err(); err != nil {
		return fmt.Errorf("failed to remove from queue: %w", err)
	}

	// Delete data
	if err := r.rdb.Del(ctx, r.blockKey(id)).Err(); err != nil {
		return fmt.Errorf("failed to delete failed block: %w", err)
	}

	return nil
}

// GetAll retrieves all failed blocks.
func (r *FailedBlockRepo) GetAll(ctx context.Context, chainID string) ([]*domain.FailedBlock, error) {
	// Get all IDs
	ids, err := r.rdb.ZRange(ctx, r.queueKey(), 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("zrange failed: %w", err)
	}

	blocks := make([]*domain.FailedBlock, 0, len(ids))
	for _, id := range ids {
		data, err := r.rdb.Get(ctx, r.blockKey(id)).Bytes()
		if err == redis.Nil {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("failed to get failed block: %w", err)
		}

		var fb domain.FailedBlock
		if err := json.Unmarshal(data, &fb); err != nil {
			continue
		}
		blocks = append(blocks, &fb)
	}

	return blocks, nil
}

// Count returns the count of failed blocks.
func (r *FailedBlockRepo) Count(ctx context.Context, chainID string) (int, error) {
	count, err := r.rdb.ZCard(ctx, r.queueKey()).Result()
	if err != nil {
		return 0, fmt.Errorf("zcard failed: %w", err)
	}
	return int(count), nil
}
