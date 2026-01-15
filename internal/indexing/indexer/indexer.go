package indexer

import (
	"context"
	"time"

	"github.com/vietddude/watcher/internal/infra/chain"
	"github.com/vietddude/watcher/internal/indexing/emitter"
	"github.com/vietddude/watcher/internal/indexing/filter"
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

type Status struct {
	ChainID         string
	CurrentBlock    uint64
	LatestBlock     uint64
	Lag             int64
	State           string
	BlocksPerSecond float64
	MissingBlocks   int
	FailedBlocks    int
}

// Config holds indexer configuration
type Config struct {
	ChainID          string
	ChainAdapter     chain.Adapter
	Filter           filter.Filter
	BlockRepo        storage.BlockRepository
	TransactionRepo  storage.TransactionRepository
	CursorRepo       storage.CursorRepository
	MissingBlockRepo storage.MissingBlockRepository
	FailedBlockRepo  storage.FailedBlockRepository
	Emitter          emitter.Emitter
	ScanInterval     time.Duration
	BatchSize        int
	FinalityBlocks   uint64
}
