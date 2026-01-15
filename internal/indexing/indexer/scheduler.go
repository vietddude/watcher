package indexer

import (
	"context"
	"time"
)

// Scheduler determines which blocks to process next
type Scheduler interface {
	// GetNextBlock returns the next block number to process
	GetNextBlock(ctx context.Context) (uint64, error)

	// GetBatchSize returns the optimal batch size based on current state
	GetBatchSize() int

	// GetScanInterval returns the delay before next scan
	GetScanInterval() time.Duration

	// ShouldCatchUp determines if we should enter catchup mode
	ShouldCatchUp(currentBlock, latestBlock uint64) bool
}
