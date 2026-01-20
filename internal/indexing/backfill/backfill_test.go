package backfill

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/storage"
)

type mockBlockRepo struct {
	gaps []Gap
}

func (r *mockBlockRepo) Save(ctx context.Context, block *domain.Block) error { return nil }
func (r *mockBlockRepo) SaveBatch(ctx context.Context, blocks []*domain.Block) error {
	return nil
}

func (r *mockBlockRepo) GetByNumber(
	ctx context.Context,
	chainID string,
	num uint64,
) (*domain.Block, error) {
	return nil, nil
}

func (r *mockBlockRepo) GetByHash(
	ctx context.Context,
	chainID, hash string,
) (*domain.Block, error) {
	return nil, nil
}
func (r *mockBlockRepo) GetLatest(ctx context.Context, chainID string) (*domain.Block, error) {
	return nil, nil
}

func (r *mockBlockRepo) UpdateStatus(
	ctx context.Context,
	chainID string,
	num uint64,
	status domain.BlockStatus,
) error {
	return nil
}

func (r *mockBlockRepo) FindGaps(
	ctx context.Context,
	chainID string,
	from, to uint64,
) ([]storage.Gap, error) {
	result := make([]storage.Gap, len(r.gaps))
	for i, g := range r.gaps {
		result[i] = storage.Gap{FromBlock: g.FromBlock, ToBlock: g.ToBlock}
	}
	return result, nil
}
func (r *mockBlockRepo) DeleteRange(ctx context.Context, chainID string, from, to uint64) error {
	return nil
}
func (r *mockBlockRepo) DeleteBlocksOlderThan(
	ctx context.Context,
	chainID string,
	timestamp uint64,
) error {
	return nil
}

type mockMissingRepo struct {
	mu       sync.Mutex
	blocks   []*domain.MissingBlock
	nextIdx  int
	addCount int
}

func (r *mockMissingRepo) Add(ctx context.Context, m *domain.MissingBlock) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.blocks = append(r.blocks, m)
	r.addCount++
	return nil
}

func (r *mockMissingRepo) GetNext(
	ctx context.Context,
	chainID string,
) (*domain.MissingBlock, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range r.blocks {
		if b.ChainID == chainID && b.Status == domain.MissingBlockStatusPending {
			return b, nil
		}
	}
	return nil, nil
}

func (r *mockMissingRepo) MarkProcessing(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range r.blocks {
		if b.ID == id {
			b.Status = domain.MissingBlockStatusProcessing
		}
	}
	return nil
}

func (r *mockMissingRepo) MarkCompleted(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range r.blocks {
		if b.ID == id {
			b.Status = domain.MissingBlockStatusCompleted
		}
	}
	return nil
}

func (r *mockMissingRepo) MarkFailed(ctx context.Context, id string, msg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range r.blocks {
		if b.ID == id {
			b.Status = domain.MissingBlockStatusFailed
		}
	}
	return nil
}

func (r *mockMissingRepo) GetPending(
	ctx context.Context,
	chainID string,
) ([]*domain.MissingBlock, error) {
	return nil, nil
}

func (r *mockMissingRepo) Count(ctx context.Context, chainID string) (int, error) {
	return len(r.blocks), nil
}

func TestDetector_OnCursorGap(t *testing.T) {
	blockRepo := &mockBlockRepo{}
	missingRepo := &mockMissingRepo{}
	detector := NewDetector(blockRepo, missingRepo)

	ctx := context.Background()

	// Simulate cursor at block 1000, new block is 1005
	err := detector.OnCursorGap(ctx, "ethereum", 1000, 1005)
	if err != nil {
		t.Fatalf("OnCursorGap failed: %v", err)
	}

	// Should have queued one missing block range
	if missingRepo.addCount != 1 {
		t.Errorf("expected 1 missing block added, got %d", missingRepo.addCount)
	}

	if len(missingRepo.blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(missingRepo.blocks))
	}

	mb := missingRepo.blocks[0]
	if mb.FromBlock != 1001 || mb.ToBlock != 1004 {
		t.Errorf("expected range [1001, 1004], got [%d, %d]", mb.FromBlock, mb.ToBlock)
	}
}

func TestDetector_OnCursorGap_NoGap(t *testing.T) {
	blockRepo := &mockBlockRepo{}
	missingRepo := &mockMissingRepo{}
	detector := NewDetector(blockRepo, missingRepo)

	ctx := context.Background()

	// Sequential blocks, no gap
	err := detector.OnCursorGap(ctx, "ethereum", 1000, 1001)
	if err != nil {
		t.Fatalf("OnCursorGap failed: %v", err)
	}

	if missingRepo.addCount != 0 {
		t.Errorf("expected 0 missing blocks for sequential, got %d", missingRepo.addCount)
	}
}

func TestDetector_ScanDatabase(t *testing.T) {
	blockRepo := &mockBlockRepo{
		gaps: []Gap{
			{FromBlock: 100, ToBlock: 105},
			{FromBlock: 200, ToBlock: 210},
		},
	}
	missingRepo := &mockMissingRepo{}
	detector := NewDetector(blockRepo, missingRepo)

	ctx := context.Background()
	gaps, err := detector.ScanDatabase(ctx, "ethereum", 0, 1000)
	if err != nil {
		t.Fatalf("ScanDatabase failed: %v", err)
	}

	if len(gaps) != 2 {
		t.Errorf("expected 2 gaps, got %d", len(gaps))
	}
}

func TestProcessor_ProcessOne_NoBlocks(t *testing.T) {
	missingRepo := &mockMissingRepo{}
	fetcher := func(chainID string, blockNum uint64) error {
		return nil
	}

	processor := NewProcessor(DefaultConfig(), missingRepo, fetcher)

	ctx := context.Background()
	err := processor.ProcessOne(ctx, "ethereum", "Ethereum")

	if err != ErrNoMissingBlocks {
		t.Errorf("expected ErrNoMissingBlocks, got: %v", err)
	}
}

func TestProcessor_ProcessOne_Success(t *testing.T) {
	missingRepo := &mockMissingRepo{
		blocks: []*domain.MissingBlock{
			{
				ID:        "test-1",
				ChainID:   "ethereum",
				FromBlock: 100,
				ToBlock:   102,
				Status:    domain.MissingBlockStatusPending,
			},
		},
	}

	fetchedBlocks := []uint64{}
	fetcher := func(chainID string, blockNum uint64) error {
		fetchedBlocks = append(fetchedBlocks, blockNum)
		return nil
	}

	config := DefaultConfig()
	config.QuotaCheckEnabled = false // Disable for test
	processor := NewProcessor(config, missingRepo, fetcher)

	ctx := context.Background()
	err := processor.ProcessOne(ctx, "ethereum", "Ethereum")
	if err != nil {
		t.Fatalf("ProcessOne failed: %v", err)
	}

	// Should have fetched blocks 100, 101, 102
	if len(fetchedBlocks) != 3 {
		t.Errorf("expected 3 blocks fetched, got %d", len(fetchedBlocks))
	}

	// Check status updated
	if missingRepo.blocks[0].Status != domain.MissingBlockStatusCompleted {
		t.Errorf("expected completed status, got %s", missingRepo.blocks[0].Status)
	}
}

func TestProcessor_RateLimiting(t *testing.T) {
	config := DefaultConfig()
	config.MinInterval = 100 * time.Millisecond
	config.QuotaCheckEnabled = false

	processor := NewProcessor(config, &mockMissingRepo{}, nil)

	// Record a processed block
	processor.recordProcessed("ethereum", "Ethereum")

	// Should not be able to process immediately
	if processor.canProcess("ethereum") {
		t.Error("should not be able to process immediately after last process")
	}

	// Wait for interval
	time.Sleep(150 * time.Millisecond)

	if !processor.canProcess("ethereum") {
		t.Error("should be able to process after interval")
	}
}
