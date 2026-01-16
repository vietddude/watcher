package memory

import (
	"context"
	"fmt"
	"sync"

	// Added for atomic ops if needed
	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/storage"
)

type MemoryStorage struct {
	blocks  map[string]*domain.Block
	txs     map[string]*domain.Transaction
	cursors map[string]*domain.Cursor
	missing map[string][]*domain.MissingBlock
	failed  map[string][]*domain.FailedBlock
	mu      sync.RWMutex
}

func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		blocks:  make(map[string]*domain.Block),
		txs:     make(map[string]*domain.Transaction),
		cursors: make(map[string]*domain.Cursor),
		missing: make(map[string][]*domain.MissingBlock),
		failed:  make(map[string][]*domain.FailedBlock),
	}
}

// -----------------------------------------------------------------------------
// Block Repository
// -----------------------------------------------------------------------------

type BlockRepo struct {
	store *MemoryStorage
}

func NewBlockRepo(store *MemoryStorage) *BlockRepo {
	return &BlockRepo{store: store}
}

func (r *BlockRepo) Save(ctx context.Context, block *domain.Block) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	key := block.ChainID + block.Hash
	r.store.blocks[key] = block
	return nil
}

func (r *BlockRepo) SaveBatch(ctx context.Context, blocks []*domain.Block) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	for _, b := range blocks {
		key := b.ChainID + b.Hash
		r.store.blocks[key] = b
	}
	return nil
}

func (r *BlockRepo) GetByNumber(ctx context.Context, chainID string, num uint64) (*domain.Block, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	for _, b := range r.store.blocks {
		if b.ChainID == chainID && b.Number == num {
			return b, nil
		}
	}
	return nil, nil
}

func (r *BlockRepo) GetByHash(ctx context.Context, chainID string, hash string) (*domain.Block, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	key := chainID + hash
	return r.store.blocks[key], nil
}

func (r *BlockRepo) GetLatest(ctx context.Context, chainID string) (*domain.Block, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	var max *domain.Block
	for _, b := range r.store.blocks {
		if b.ChainID == chainID {
			if max == nil || b.Number > max.Number {
				max = b
			}
		}
	}
	return max, nil
}

func (r *BlockRepo) UpdateStatus(ctx context.Context, chainID string, num uint64, status domain.BlockStatus) error {
	b, err := r.GetByNumber(ctx, chainID, num)
	if err != nil {
		return err
	}
	if b != nil {
		b.Status = status
		return r.Save(ctx, b)
	}
	return nil
}

func (r *BlockRepo) FindGaps(ctx context.Context, chainID string, from, to uint64) ([]storage.Gap, error) {
	return nil, nil // TODO: Implement gap finding
}

func (r *BlockRepo) DeleteRange(ctx context.Context, chainID string, from, to uint64) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	for k, b := range r.store.blocks {
		if b.ChainID == chainID && b.Number >= from && b.Number <= to {
			delete(r.store.blocks, k)
		}
	}
	return nil
}

// -----------------------------------------------------------------------------
// Transaction Repository
// -----------------------------------------------------------------------------

type TxRepo struct {
	store *MemoryStorage
}

func NewTxRepo(store *MemoryStorage) *TxRepo {
	return &TxRepo{store: store}
}

func (r *TxRepo) Save(ctx context.Context, tx *domain.Transaction) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	key := tx.ChainID + tx.TxHash
	r.store.txs[key] = tx
	return nil
}

func (r *TxRepo) SaveBatch(ctx context.Context, txs []*domain.Transaction) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	for _, tx := range txs {
		key := tx.ChainID + tx.TxHash
		r.store.txs[key] = tx
	}
	return nil
}

func (r *TxRepo) GetByHash(ctx context.Context, chainID string, hash string) (*domain.Transaction, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	key := chainID + hash
	return r.store.txs[key], nil
}

func (r *TxRepo) GetByBlock(ctx context.Context, chainID string, num uint64) ([]*domain.Transaction, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	var txs []*domain.Transaction
	for _, tx := range r.store.txs {
		if tx.ChainID == chainID && tx.BlockNumber == num {
			txs = append(txs, tx)
		}
	}
	return txs, nil
}

func (r *TxRepo) UpdateStatus(ctx context.Context, chainID string, hash string, status domain.TxStatus) error {
	tx, err := r.GetByHash(ctx, chainID, hash)
	if err != nil {
		return err
	}
	if tx != nil {
		tx.Status = status
		return r.Save(ctx, tx)
	}
	return nil
}

func (r *TxRepo) DeleteByBlock(ctx context.Context, chainID string, num uint64) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	for k, tx := range r.store.txs {
		if tx.ChainID == chainID && tx.BlockNumber == num {
			delete(r.store.txs, k)
		}
	}
	return nil
}

// -----------------------------------------------------------------------------
// Cursor Repository
// -----------------------------------------------------------------------------

type CursorRepo struct {
	store *MemoryStorage
}

func NewCursorRepo(store *MemoryStorage) *CursorRepo {
	return &CursorRepo{store: store}
}

func (r *CursorRepo) Get(ctx context.Context, chainID string) (*domain.Cursor, error) {
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	if c, ok := r.store.cursors[chainID]; ok {
		// Return copy
		copy := *c
		copy.Metadata = make(map[string]any) // Deep copy metadata if needed
		return &copy, nil
	}
	return nil, storage.ErrCursorNotFound
}

func (r *CursorRepo) Save(ctx context.Context, cursor *domain.Cursor) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	r.store.cursors[cursor.ChainID] = cursor
	return nil
}

func (r *CursorRepo) UpdateBlock(ctx context.Context, chainID string, num uint64, hash string) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if c, ok := r.store.cursors[chainID]; ok {
		c.CurrentBlock = num
		c.CurrentBlockHash = hash
		return nil
	}
	return fmt.Errorf("cursor not found")
}

func (r *CursorRepo) UpdateState(ctx context.Context, chainID string, state domain.CursorState) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if c, ok := r.store.cursors[chainID]; ok {
		c.State = state
		return nil
	}
	return fmt.Errorf("cursor not found")
}

func (r *CursorRepo) Rollback(ctx context.Context, chainID string, num uint64, hash string) error {
	return r.UpdateBlock(ctx, chainID, num, hash)
}

// -----------------------------------------------------------------------------
// Missing Block Repository (Minimal)
// -----------------------------------------------------------------------------

type MissingRepo struct{ store *MemoryStorage }

func NewMissingRepo(s *MemoryStorage) *MissingRepo                           { return &MissingRepo{store: s} }
func (r *MissingRepo) Add(ctx context.Context, m *domain.MissingBlock) error { return nil }
func (r *MissingRepo) GetNext(ctx context.Context, c string) (*domain.MissingBlock, error) {
	return nil, nil
}
func (r *MissingRepo) MarkProcessing(ctx context.Context, id string) error  { return nil }
func (r *MissingRepo) MarkCompleted(ctx context.Context, id string) error   { return nil }
func (r *MissingRepo) MarkFailed(ctx context.Context, id, msg string) error { return nil }
func (r *MissingRepo) GetPending(ctx context.Context, c string) ([]*domain.MissingBlock, error) {
	return nil, nil
}
func (r *MissingRepo) Count(ctx context.Context, c string) (int, error) { return 0, nil }

// -----------------------------------------------------------------------------
// Failed Block Repository (Minimal)
// -----------------------------------------------------------------------------

type FailedRepo struct{ store *MemoryStorage }

func NewFailedRepo(s *MemoryStorage) *FailedRepo                           { return &FailedRepo{store: s} }
func (r *FailedRepo) Add(ctx context.Context, f *domain.FailedBlock) error { return nil }
func (r *FailedRepo) GetNext(ctx context.Context, c string) (*domain.FailedBlock, error) {
	return nil, nil
}
func (r *FailedRepo) IncrementRetry(ctx context.Context, id string) error { return nil }
func (r *FailedRepo) MarkResolved(ctx context.Context, id string) error   { return nil }
func (r *FailedRepo) GetAll(ctx context.Context, c string) ([]*domain.FailedBlock, error) {
	return nil, nil
}
func (r *FailedRepo) Count(ctx context.Context, c string) (int, error) { return 0, nil }
