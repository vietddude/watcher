package throttle

import (
	"context"
	"sync"
	"time"

	"github.com/vietddude/watcher/internal/infra/chain"
)

// HeadCache caches the result of GetLatestBlock to reduce redundant API calls.
// This is particularly useful when the indexer is at the chain head and polling frequently.
type HeadCache struct {
	adapter chain.Adapter
	ttl     time.Duration

	mu       sync.RWMutex
	cached   uint64
	cachedAt time.Time
}

// NewHeadCache creates a new head cache with the given TTL.
func NewHeadCache(adapter chain.Adapter, ttl time.Duration) *HeadCache {
	return &HeadCache{
		adapter: adapter,
		ttl:     ttl,
	}
}

// GetLatestBlock returns the cached chain head if within TTL, otherwise fetches fresh.
func (c *HeadCache) GetLatestBlock(ctx context.Context) (uint64, error) {
	// Check cache first
	c.mu.RLock()
	if time.Since(c.cachedAt) < c.ttl && c.cached > 0 {
		cached := c.cached
		c.mu.RUnlock()
		return cached, nil
	}
	c.mu.RUnlock()

	// Cache miss or expired - fetch fresh
	head, err := c.adapter.GetLatestBlock(ctx)
	if err != nil {
		return 0, err
	}

	// Update cache
	c.mu.Lock()
	c.cached = head
	c.cachedAt = time.Now()
	c.mu.Unlock()

	return head, nil
}

// Invalidate clears the cache, forcing the next call to fetch fresh data.
func (c *HeadCache) Invalidate() {
	c.mu.Lock()
	c.cachedAt = time.Time{}
	c.mu.Unlock()
}
