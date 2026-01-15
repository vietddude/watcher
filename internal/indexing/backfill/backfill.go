// Package backfill handles missing block detection and recovery.
//
// # Design: Minimize RPC Calls
//
// Gap detection uses database only (0 RPC calls):
//   - Cursor gap: cursor.Advance() returns ErrBlockGap
//   - DB scan: BlockRepository.FindGaps() queries stored blocks
//
// RPC is only called when actually fetching a missing block.
//
// # Rate Limiting
//
// Default: 5 blocks/minute, pauses if RPC quota > 70%.
//
// # Usage
//
//	detector := backfill.NewDetector(blockRepo, missingRepo)
//	processor := backfill.NewProcessor(backfill.DefaultConfig(), missingRepo, fetcher, budget)
//
//	// When cursor returns ErrBlockGap
//	detector.OnCursorGap(ctx, "ethereum", 1000, 1005)
//
//	// Background processing
//	go processor.Run(ctx, "ethereum")
package backfill

import (
	"github.com/vietddude/watcher/internal/infra/storage"
)

// Gap represents a range of missing blocks.
type Gap struct {
	FromBlock uint64
	ToBlock   uint64
}

// BlockFetcher is called to fetch and process a single block.
type BlockFetcher func(chainID string, blockNum uint64) error

// NewDetector creates a new gap detector.
func NewDetector(blockRepo storage.BlockRepository, missingRepo storage.MissingBlockRepository) *Detector {
	return &Detector{
		blockRepo:   blockRepo,
		missingRepo: missingRepo,
	}
}

// NewProcessor creates a new processor with the given configuration.
func NewProcessor(config ProcessorConfig, missingRepo storage.MissingBlockRepository, fetcher BlockFetcher) *Processor {
	return &Processor{
		config:      config,
		missingRepo: missingRepo,
		fetcher:     fetcher,
	}
}
