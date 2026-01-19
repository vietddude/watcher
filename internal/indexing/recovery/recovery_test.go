package recovery

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
)

// =============================================================================
// Mock Repository
// =============================================================================

type mockFailedRepo struct {
	mu     sync.Mutex
	blocks []*domain.FailedBlock
}

func (r *mockFailedRepo) Add(ctx context.Context, b *domain.FailedBlock) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.blocks = append(r.blocks, b)
	return nil
}

func (r *mockFailedRepo) GetNext(ctx context.Context, chainID string) (*domain.FailedBlock, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.blocks) > 0 {
		return r.blocks[0], nil
	}
	return nil, nil // Queue empty
}

func (r *mockFailedRepo) IncrementRetry(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range r.blocks {
		if b.ID == id {
			b.RetryCount++
		}
	}
	return nil
}

func (r *mockFailedRepo) MarkResolved(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Remove from list for simplicity in mock
	newBlocks := make([]*domain.FailedBlock, 0)
	for _, b := range r.blocks {
		if b.ID != id {
			newBlocks = append(newBlocks, b)
		}
	}
	r.blocks = newBlocks
	return nil
}

func (r *mockFailedRepo) GetAll(
	ctx context.Context,
	chainID string,
) ([]*domain.FailedBlock, error) {
	return r.blocks, nil
}
func (r *mockFailedRepo) Count(ctx context.Context, chainID string) (int, error) {
	return len(r.blocks), nil
}

// =============================================================================
// Strategy Tests
// =============================================================================

func TestBackoff_Delay(t *testing.T) {
	strategy := DefaultBackoff(nil)
	strategy.InitialDelay = 1 * time.Second
	strategy.MaxDelay = 10 * time.Second

	// Attempt 0: 1*2^0 = 1s
	if d := strategy.GetDelay(0); d != 1*time.Second {
		t.Errorf("expected 1s, got %v", d)
	}

	// Attempt 1: 1*2^1 = 2s
	if d := strategy.GetDelay(1); d != 2*time.Second {
		t.Errorf("expected 2s, got %v", d)
	}

	// Attempt 2: 1*2^2 = 4s
	if d := strategy.GetDelay(2); d != 4*time.Second {
		t.Errorf("expected 4s, got %v", d)
	}

	// Attempt 10: Cap at MaxDelay (10s)
	if d := strategy.GetDelay(10); d != 10*time.Second {
		t.Errorf("expected 10s, got %v", d)
	}
}

func TestBackoff_ShouldRetry(t *testing.T) {
	strategy := DefaultBackoff(nil)
	strategy.MaxAttempts = 3

	if !strategy.ShouldRetry(errors.New("err"), 0) {
		t.Error("should retry attempt 0")
	}
	if !strategy.ShouldRetry(errors.New("err"), 2) {
		t.Error("should retry attempt 2")
	}
	if strategy.ShouldRetry(errors.New("err"), 3) {
		t.Error("should NOT retry attempt 3 (max reached)")
	}
}

// =============================================================================
// Handler Tests
// =============================================================================

func TestHandler_HandleFailure(t *testing.T) {
	repo := &mockFailedRepo{}
	handler := NewHandler(repo, nil, DefaultBackoff(nil))
	ctx := context.Background()

	err := handler.HandleFailure(ctx, "ethereum", 100, errors.New("rpc error"))
	if err != nil {
		t.Fatalf("HandleFailure failed: %v", err)
	}

	if len(repo.blocks) != 1 {
		t.Fatalf("expected 1 failed block, got %d", len(repo.blocks))
	}
	if repo.blocks[0].BlockNumber != 100 {
		t.Errorf("expected block 100, got %d", repo.blocks[0].BlockNumber)
	}
}

func TestHandler_ProcessNext_Success(t *testing.T) {
	repo := &mockFailedRepo{}
	// Add a block that is ready to be retried (LastAttempt 1 hour ago)
	repo.blocks = append(repo.blocks, &domain.FailedBlock{
		ID:          "fail-1",
		BlockNumber: 100,
		RetryCount:  0,
		LastAttempt: time.Now().Add(-1 * time.Hour),
	})

	fetcher := func(ctx context.Context, chainID string, blockNum uint64) error {
		return nil // Success
	}

	handler := NewHandler(repo, fetcher, DefaultBackoff(nil))
	ctx := context.Background()

	err := handler.ProcessNext(ctx, "ethereum")
	if err != nil {
		t.Fatalf("ProcessNext failed: %v", err)
	}

	if len(repo.blocks) != 0 {
		t.Error("expected block to be removed after success")
	}
}

func TestHandler_ProcessNext_Wait(t *testing.T) {
	repo := &mockFailedRepo{}
	// Add a block that was JUST attempted (should wait backoff)
	repo.blocks = append(repo.blocks, &domain.FailedBlock{
		ID:          "fail-1",
		BlockNumber: 100,
		RetryCount:  0,
		LastAttempt: time.Now(), // Just now
	})

	called := false
	fetcher := func(ctx context.Context, chainID string, blockNum uint64) error {
		called = true
		return nil
	}

	handler := NewHandler(repo, fetcher, DefaultBackoff(nil))

	// Initial delay is 2s, so "Just now" + 2s > Now. Should skip.
	err := handler.ProcessNext(context.Background(), "ethereum")
	if err != nil {
		t.Fatalf("ProcessNext failed: %v", err)
	}

	if called {
		t.Error("should NOT have called fetcher (too early)")
	}
}

func TestHandler_ProcessNext_FailAndIncrement(t *testing.T) {
	repo := &mockFailedRepo{}
	repo.blocks = append(repo.blocks, &domain.FailedBlock{
		ID:          "fail-1",
		BlockNumber: 100,
		RetryCount:  0,
		LastAttempt: time.Now().Add(-1 * time.Hour),
	})

	fetcher := func(ctx context.Context, chainID string, blockNum uint64) error {
		return errors.New("still failing")
	}

	handler := NewHandler(repo, fetcher, DefaultBackoff(nil))

	err := handler.ProcessNext(context.Background(), "ethereum")
	if err != nil {
		t.Fatalf("ProcessNext failed: %v", err)
	}

	if len(repo.blocks) != 1 {
		t.Error("block should stay in queue")
	}
	if repo.blocks[0].RetryCount != 1 {
		t.Errorf("expected retry count 1, got %d", repo.blocks[0].RetryCount)
	}
}
