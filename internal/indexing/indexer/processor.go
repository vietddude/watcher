package indexer

import (
	"context"

	"github.com/vietddude/watcher/internal/core/domain"
)

// Processor handles the block processing pipeline
type Processor interface {
	// ProcessBlock processes a single block
	ProcessBlock(ctx context.Context, blockNumber uint64) error

	// ProcessBatch processes multiple blocks
	ProcessBatch(ctx context.Context, fromBlock, toBlock uint64) error

	// ProcessBlockData processes already fetched block data
	ProcessBlockData(ctx context.Context, block *domain.Block, txs []*domain.Transaction) error
}
