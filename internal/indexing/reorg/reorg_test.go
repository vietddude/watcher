package reorg

import (
	"context"
	"sync"
	"testing"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/storage"
)

type mockBlockRepo struct {
	mu     sync.RWMutex
	blocks map[uint64]*domain.Block
}

func newMockBlockRepo() *mockBlockRepo {
	return &mockBlockRepo{
		blocks: make(map[uint64]*domain.Block),
	}
}

func (r *mockBlockRepo) addBlock(num uint64, hash, parentHash string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.blocks[num] = &domain.Block{
		ChainID:    "ethereum",
		Number:     num,
		Hash:       hash,
		ParentHash: parentHash,
		Status:     domain.BlockStatusProcessed,
	}
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
	r.mu.RLock()
	defer r.mu.RUnlock()
	if b, ok := r.blocks[num]; ok {
		copy := *b
		return &copy, nil
	}
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
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.blocks[num]; ok {
		b.Status = status
	}
	return nil
}

func (r *mockBlockRepo) FindGaps(
	ctx context.Context,
	chainID string,
	from, to uint64,
) ([]storage.Gap, error) {
	return nil, nil
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

func TestDetector_ParentHashMatch(t *testing.T) {
	repo := newMockBlockRepo()
	// Chain: 999 -> 1000 -> 1001
	repo.addBlock(999, "hash_999", "hash_998")
	repo.addBlock(1000, "hash_1000", "hash_999")

	detector := NewDetector(Config{}, repo)
	ctx := context.Background()

	// New block 1001 with correct parent hash
	info, err := detector.CheckParentHash(ctx, "ethereum", 1001, "hash_1000")
	if err != nil {
		t.Fatalf("CheckParentHash failed: %v", err)
	}

	if info.Detected {
		t.Error("expected no reorg detected")
	}
}

func TestDetector_ParentHashMismatch(t *testing.T) {
	repo := newMockBlockRepo()
	// Chain: 999 -> 1000
	repo.addBlock(999, "hash_999", "hash_998")
	repo.addBlock(1000, "hash_1000", "hash_999")

	detector := NewDetector(Config{}, repo)
	ctx := context.Background()

	// New block 1001 with WRONG parent hash (reorg happened)
	info, err := detector.CheckParentHash(ctx, "ethereum", 1001, "hash_1000_DIFFERENT")
	if err != nil {
		t.Fatalf("CheckParentHash failed: %v", err)
	}

	if !info.Detected {
		t.Error("expected reorg to be detected")
	}
	if info.Depth < 1 {
		t.Errorf("expected depth >= 1, got %d", info.Depth)
	}
}

func TestDetector_NoStoredBlock(t *testing.T) {
	repo := newMockBlockRepo()
	detector := NewDetector(Config{}, repo)
	ctx := context.Background()

	// Block 1001 with no stored block 1000
	info, err := detector.CheckParentHash(ctx, "ethereum", 1001, "any_hash")
	if err != nil {
		t.Fatalf("CheckParentHash failed: %v", err)
	}

	if info.Detected {
		t.Error("expected no reorg when stored block doesn't exist")
	}
}

func TestDetector_BlockZero(t *testing.T) {
	repo := newMockBlockRepo()
	detector := NewDetector(Config{}, repo)
	ctx := context.Background()

	// Block 0 should never trigger reorg
	info, err := detector.CheckParentHash(ctx, "ethereum", 0, "genesis_parent")
	if err != nil {
		t.Fatalf("CheckParentHash failed: %v", err)
	}

	if info.Detected {
		t.Error("expected no reorg for block 0")
	}
}
