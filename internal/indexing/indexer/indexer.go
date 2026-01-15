package indexer

import (
	"context"
	"time"

	"github.com/vietddude/watcher/internal/core/cursor"
	"github.com/vietddude/watcher/internal/indexing/emitter"
	"github.com/vietddude/watcher/internal/indexing/filter"
	"github.com/vietddude/watcher/internal/indexing/recovery"
	"github.com/vietddude/watcher/internal/indexing/reorg"
	"github.com/vietddude/watcher/internal/infra/chain"
	"github.com/vietddude/watcher/internal/infra/storage"
)

// Indexer is the main orchestrator that coordinates all components
type Indexer interface {
	// Start begins the indexing process
	Start(ctx context.Context) error

	// Stop gracefully stops the indexer
	Stop() error

	// GetStatus returns current indexing status
	GetStatus() Status
}

// Config holds all dependencies for the pipeline
type Config struct {
	ChainID      string
	ChainAdapter chain.Adapter
	Cursor       cursor.Manager
	Reorg        *reorg.Detector
	ReorgHandler *reorg.Handler
	Recovery     *recovery.Handler
	Emitter      emitter.Emitter
	Filter       filter.Filter

	// Repositories
	BlockRepo       storage.BlockRepository
	TransactionRepo storage.TransactionRepository

	// Settings
	ScanInterval time.Duration
	BatchSize    int
}

// Status represents the current state of the indexer
type Status struct {
	ChainID      string
	CurrentBlock uint64
	LatestBlock  uint64
	Lag          int64
	Running      bool
}
