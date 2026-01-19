package redis

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client wraps Redis operations for the rescan pipeline.
type Client struct {
	rdb *redis.Client
}

// Config holds Redis connection configuration.
type Config struct {
	URL      string `yaml:"url"`
	Password string `yaml:"password"`
}

// NewClient creates a new Redis client.
func NewClient(cfg Config) (*Client, error) {
	opts, err := redis.ParseURL(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse redis URL: %w", err)
	}
	if cfg.Password != "" {
		opts.Password = cfg.Password
	}

	rdb := redis.NewClient(opts)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	return &Client{rdb: rdb}, nil
}

// Close closes the Redis connection.
func (c *Client) Close() error {
	return c.rdb.Close()
}

// Key helpers
func queueKey(internalCode string) string {
	return fmt.Sprintf("missing_blocks:%s", internalCode)
}

func lockKey(internalCode string, start, end uint64) string {
	return fmt.Sprintf("processing:%s:%d-%d", internalCode, start, end)
}

func progressKey(internalCode string, start, end uint64) string {
	return fmt.Sprintf("processed:%s:%d-%d", internalCode, start, end)
}

// PopRange pops the next range from the queue (lowest score = oldest/smallest block).
func (c *Client) PopRange(
	ctx context.Context,
	internalCode string,
) (start, end uint64, found bool, err error) {
	key := queueKey(internalCode)

	// Get the first element (lowest score)
	results, err := c.rdb.ZRangeWithScores(ctx, key, 0, 0).Result()
	if err != nil {
		return 0, 0, false, fmt.Errorf("zrange failed: %w", err)
	}

	if len(results) == 0 {
		return 0, 0, false, nil
	}

	member := results[0].Member.(string)
	start, end, err = ParseRangeString(member)
	if err != nil {
		return 0, 0, false, fmt.Errorf("invalid range format: %w", err)
	}

	// Remove from queue
	if err := c.rdb.ZRem(ctx, key, member).Err(); err != nil {
		return 0, 0, false, fmt.Errorf("zrem failed: %w", err)
	}

	return start, end, true, nil
}

// PushRange adds a range to the queue.
func (c *Client) PushRange(ctx context.Context, internalCode string, start, end uint64) error {
	key := queueKey(internalCode)
	member := fmt.Sprintf("%d-%d", start, end)
	score := float64(start)

	if err := c.rdb.ZAdd(ctx, key, redis.Z{Score: score, Member: member}).Err(); err != nil {
		return fmt.Errorf("zadd failed: %w", err)
	}
	return nil
}

// GetAllRanges returns all ranges in the queue.
func (c *Client) GetAllRanges(ctx context.Context, internalCode string) ([]string, error) {
	key := queueKey(internalCode)
	return c.rdb.ZRange(ctx, key, 0, -1).Result()
}

// ClearQueue removes all ranges from the queue (for merging).
func (c *Client) ClearQueue(ctx context.Context, internalCode string) error {
	key := queueKey(internalCode)
	return c.rdb.Del(ctx, key).Err()
}

// AcquireLock attempts to acquire a processing lock for a range.
func (c *Client) AcquireLock(
	ctx context.Context,
	internalCode string,
	start, end uint64,
	ttl time.Duration,
) (bool, error) {
	key := lockKey(internalCode, start, end)
	ok, err := c.rdb.SetNX(ctx, key, "locked", ttl).Result()
	if err != nil {
		return false, fmt.Errorf("setnx failed: %w", err)
	}
	return ok, nil
}

// ReleaseLock releases a processing lock.
func (c *Client) ReleaseLock(ctx context.Context, internalCode string, start, end uint64) error {
	key := lockKey(internalCode, start, end)
	return c.rdb.Del(ctx, key).Err()
}

// RefreshLock extends the TTL of a lock.
func (c *Client) RefreshLock(
	ctx context.Context,
	internalCode string,
	start, end uint64,
	ttl time.Duration,
) error {
	key := lockKey(internalCode, start, end)
	return c.rdb.Expire(ctx, key, ttl).Err()
}

// GetProgress gets the last processed block for a range.
func (c *Client) GetProgress(
	ctx context.Context,
	internalCode string,
	start, end uint64,
) (uint64, error) {
	key := progressKey(internalCode, start, end)
	val, err := c.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return start, nil // No progress, start from beginning
	}
	if err != nil {
		return 0, fmt.Errorf("get failed: %w", err)
	}
	return strconv.ParseUint(val, 10, 64)
}

// SetProgress sets the last processed block for a range.
func (c *Client) SetProgress(
	ctx context.Context,
	internalCode string,
	start, end, current uint64,
	ttl time.Duration,
) error {
	key := progressKey(internalCode, start, end)
	return c.rdb.Set(ctx, key, strconv.FormatUint(current, 10), ttl).Err()
}

// ClearProgress removes progress tracking for a range.
func (c *Client) ClearProgress(ctx context.Context, internalCode string, start, end uint64) error {
	key := progressKey(internalCode, start, end)
	return c.rdb.Del(ctx, key).Err()
}

// ParseRangeString parses "12000-12500" format.
func ParseRangeString(s string) (start, end uint64, err error) {
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid range format: %s", s)
	}

	start, err = strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid start: %w", err)
	}

	end, err = strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid end: %w", err)
	}

	if start > end {
		return 0, 0, fmt.Errorf("start > end: %d > %d", start, end)
	}

	return start, end, nil
}
