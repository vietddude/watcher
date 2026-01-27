package throttle

import (
	"context"
	"testing"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
)

// mockAdapter implements chain.Adapter for testing
type mockAdapter struct {
	latestBlock uint64
	callCount   int
}

func (m *mockAdapter) GetLatestBlock(ctx context.Context) (uint64, error) {
	m.callCount++
	return m.latestBlock, nil
}

func (m *mockAdapter) GetBlock(ctx context.Context, blockNumber uint64) (*domain.Block, error) {
	return nil, nil
}

func (m *mockAdapter) GetBlockByHash(ctx context.Context, blockHash string) (*domain.Block, error) {
	return nil, nil
}

func (m *mockAdapter) GetTransactions(
	ctx context.Context,
	block *domain.Block,
) ([]*domain.Transaction, error) {
	return nil, nil
}

func (m *mockAdapter) FilterTransactions(
	ctx context.Context,
	txs []*domain.Transaction,
	addresses []string,
) ([]*domain.Transaction, error) {
	return nil, nil
}

func (m *mockAdapter) VerifyBlockHash(
	ctx context.Context,
	blockNumber uint64,
	expectedHash string,
) (bool, error) {
	return true, nil
}

func (m *mockAdapter) EnrichTransaction(ctx context.Context, tx *domain.Transaction) error {
	return nil
}

func (m *mockAdapter) GetFinalityDepth() uint64 {
	return 12
}

func (m *mockAdapter) GetChainID() domain.ChainID {
	return domain.EthereumMainnet
}

func (m *mockAdapter) SupportsBloomFilter() bool {
	return true
}

func TestHeadCache_CachesResult(t *testing.T) {
	adapter := &mockAdapter{latestBlock: 1000}
	cache := NewHeadCache(adapter, 3*time.Second)

	ctx := context.Background()

	// First call - should hit adapter
	result1, err := cache.GetLatestBlock(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result1 != 1000 {
		t.Errorf("expected 1000, got %d", result1)
	}
	if adapter.callCount != 1 {
		t.Errorf("expected 1 adapter call, got %d", adapter.callCount)
	}

	// Second call within TTL - should use cache
	result2, err := cache.GetLatestBlock(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result2 != 1000 {
		t.Errorf("expected 1000, got %d", result2)
	}
	if adapter.callCount != 1 {
		t.Errorf("expected still 1 adapter call (cached), got %d", adapter.callCount)
	}
}

func TestHeadCache_ExpiresAfterTTL(t *testing.T) {
	adapter := &mockAdapter{latestBlock: 1000}
	cache := NewHeadCache(adapter, 100*time.Millisecond)

	ctx := context.Background()

	// First call
	_, err := cache.GetLatestBlock(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)

	// Update adapter value
	adapter.latestBlock = 1001

	// Second call after TTL - should fetch fresh
	result, err := cache.GetLatestBlock(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 1001 {
		t.Errorf("expected fresh value 1001, got %d", result)
	}
	if adapter.callCount != 2 {
		t.Errorf("expected 2 adapter calls, got %d", adapter.callCount)
	}
}

func TestHeadCache_Invalidate(t *testing.T) {
	adapter := &mockAdapter{latestBlock: 1000}
	cache := NewHeadCache(adapter, 3*time.Second)

	ctx := context.Background()

	// First call
	_, err := cache.GetLatestBlock(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Invalidate cache
	cache.Invalidate()

	// Update adapter value
	adapter.latestBlock = 1001

	// Next call should fetch fresh even though TTL hasn't expired
	result, err := cache.GetLatestBlock(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 1001 {
		t.Errorf("expected fresh value 1001 after invalidate, got %d", result)
	}
	if adapter.callCount != 2 {
		t.Errorf("expected 2 adapter calls, got %d", adapter.callCount)
	}
}
