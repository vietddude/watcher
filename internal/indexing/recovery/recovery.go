package recovery

import (
	"context"

	"github.com/vietddude/watcher/internal/core/domain"
)

// MissingBlockDetector detects and handles missing blocks
type MissingBlockDetector interface {
	// DetectGaps detects missing blocks in a range
	DetectGaps(ctx context.Context, fromBlock, toBlock uint64) error

	// ProcessNext processes the next missing block in queue
	ProcessNext(ctx context.Context) error

	// BackfillRange backfills a specific range
	BackfillRange(ctx context.Context, fromBlock, toBlock uint64) error
}

// ReorgDetector detects and handles blockchain reorganizations
type ReorgDetector interface {
	// CheckForReorg checks if a reorg has occurred
	CheckForReorg(ctx context.Context, block *domain.Block) (bool, uint64, error)

	// HandleReorg handles a detected reorganization
	HandleReorg(ctx context.Context, reorgDepth uint64, safeBlock uint64) error

	// VerifyFinality verifies blocks in the finality window
	VerifyFinality(ctx context.Context, currentBlock uint64) error
}

// FailedBlockHandler handles failed block retries
type FailedBlockHandler interface {
	// ProcessNext retries the next failed block
	ProcessNext(ctx context.Context) error

	// Retry retries a specific failed block
	Retry(ctx context.Context, blockNumber uint64) error
}
