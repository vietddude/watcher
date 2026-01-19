package storage

import (
	"context"
	"errors"

	"github.com/vietddude/watcher/internal/core/domain"
)

var (
	// ErrCursorNotFound is returned when a cursor doesn't exist
	ErrCursorNotFound = errors.New("cursor not found")
)

// BlockRepository handles block storage operations
type BlockRepository interface {
	// Save saves a block
	Save(ctx context.Context, block *domain.Block) error

	// SaveBatch saves multiple blocks
	SaveBatch(ctx context.Context, blocks []*domain.Block) error

	// GetByNumber retrieves a block by number
	GetByNumber(ctx context.Context, chainID string, blockNumber uint64) (*domain.Block, error)

	// GetByHash retrieves a block by hash
	GetByHash(ctx context.Context, chainID string, blockHash string) (*domain.Block, error)

	// GetLatest retrieves the latest indexed block
	GetLatest(ctx context.Context, chainID string) (*domain.Block, error)

	// UpdateStatus updates block status
	UpdateStatus(
		ctx context.Context,
		chainID string,
		blockNumber uint64,
		status domain.BlockStatus,
	) error

	// FindGaps finds missing blocks in a range
	FindGaps(ctx context.Context, chainID string, fromBlock, toBlock uint64) ([]Gap, error)

	// DeleteRange deletes blocks in a range (for reorg rollback)
	DeleteRange(ctx context.Context, chainID string, fromBlock, toBlock uint64) error
}

type Gap struct {
	FromBlock uint64
	ToBlock   uint64
}

// TransactionRepository handles transaction storage operations
type TransactionRepository interface {
	// Save saves a transaction
	Save(ctx context.Context, tx *domain.Transaction) error

	// SaveBatch saves multiple transactions
	SaveBatch(ctx context.Context, txs []*domain.Transaction) error

	// GetByHash retrieves a transaction by hash
	GetByHash(ctx context.Context, chainID string, txHash string) (*domain.Transaction, error)

	// GetByBlock retrieves all transactions in a block
	GetByBlock(
		ctx context.Context,
		chainID string,
		blockNumber uint64,
	) ([]*domain.Transaction, error)

	// UpdateStatus updates transaction status (for reorg)
	UpdateStatus(ctx context.Context, chainID string, txHash string, status domain.TxStatus) error

	// DeleteByBlock deletes transactions in a block (for reorg rollback)
	DeleteByBlock(ctx context.Context, chainID string, blockNumber uint64) error
}

// CursorRepository handles cursor storage operations
type CursorRepository interface {
	// Get retrieves the cursor for a chain
	Get(ctx context.Context, chainID string) (*domain.Cursor, error)

	// Save saves/updates the cursor
	Save(ctx context.Context, cursor *domain.Cursor) error

	// UpdateBlock updates cursor to a new block (atomic operation)
	UpdateBlock(ctx context.Context, chainID string, blockNumber uint64, blockHash string) error

	// UpdateState updates cursor state
	UpdateState(ctx context.Context, chainID string, state domain.CursorState) error

	// Rollback rolls back cursor to a previous block
	Rollback(ctx context.Context, chainID string, blockNumber uint64, blockHash string) error
}

// MissingBlockRepository handles missing blocks queue
type MissingBlockRepository interface {
	// Add adds a missing block range
	Add(ctx context.Context, missingBlock *domain.MissingBlock) error

	// GetNext retrieves the next missing block to process
	GetNext(ctx context.Context, chainID string) (*domain.MissingBlock, error)

	// MarkProcessing marks a range as being processed
	MarkProcessing(ctx context.Context, id string) error

	// MarkCompleted marks a range as completed
	MarkCompleted(ctx context.Context, id string) error

	// MarkFailed marks a range as failed
	MarkFailed(ctx context.Context, id string, errorMsg string) error

	// GetPending retrieves all pending missing blocks
	GetPending(ctx context.Context, chainID string) ([]*domain.MissingBlock, error)

	// Count returns the count of missing blocks
	Count(ctx context.Context, chainID string) (int, error)
}

// FailedBlockRepository handles failed blocks queue
type FailedBlockRepository interface {
	// Add adds a failed block
	Add(ctx context.Context, failedBlock *domain.FailedBlock) error

	// GetNext retrieves the next failed block to retry
	GetNext(ctx context.Context, chainID string) (*domain.FailedBlock, error)

	// IncrementRetry increments retry count
	IncrementRetry(ctx context.Context, id string) error

	// MarkResolved removes a failed block (successfully retried)
	MarkResolved(ctx context.Context, id string) error

	// GetAll retrieves all failed blocks
	GetAll(ctx context.Context, chainID string) ([]*domain.FailedBlock, error)

	// Count returns the count of failed blocks
	Count(ctx context.Context, chainID string) (int, error)
}

// WalletRepository handles wallet address storage
type WalletRepository interface {
	// Save saves a wallet address
	Save(ctx context.Context, wallet *domain.WalletAddress) error

	// GetByAddress retrieves a wallet by address
	GetByAddress(ctx context.Context, address string) (*domain.WalletAddress, error)

	// GetAll retrieves all wallet addresses
	GetAll(ctx context.Context) ([]*domain.WalletAddress, error)
}
