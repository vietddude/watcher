package rescan

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/vietddude/watcher/internal/infra/chain"
	redisclient "github.com/vietddude/watcher/internal/infra/redis"
	"github.com/vietddude/watcher/internal/infra/storage"
)

// WorkerConfig holds configuration for the rescan worker.
type WorkerConfig struct {
	ChunkSize   uint64        // Max blocks per chunk (default: 100)
	LockTTL     time.Duration // Lock TTL (default: 60s)
	ProgressTTL time.Duration // Progress TTL (default: 5m)
	EmptySleep  time.Duration // Sleep when queue empty (default: 10s)
	ScanTimeout time.Duration // Max time per range (default: 5m)
}

// DefaultConfig returns default worker configuration.
func DefaultConfig() WorkerConfig {
	return WorkerConfig{
		ChunkSize:   100,
		LockTTL:     60 * time.Second,
		ProgressTTL: 5 * time.Minute,
		EmptySleep:  10 * time.Second,
		ScanTimeout: 5 * time.Minute,
	}
}

// Worker processes rescan ranges from Redis.
type Worker struct {
	cfg          WorkerConfig
	internalCode string
	redis        *redisclient.Client
	adapter      chain.Adapter
	blockRepo    storage.BlockRepository
	txRepo       storage.TransactionRepository
	log          *slog.Logger
}

// NewWorker creates a new rescan worker.
func NewWorker(
	cfg WorkerConfig,
	internalCode string,
	redis *redisclient.Client,
	adapter chain.Adapter,
	blockRepo storage.BlockRepository,
	txRepo storage.TransactionRepository,
) *Worker {
	return &Worker{
		cfg:          cfg,
		internalCode: internalCode,
		redis:        redis,
		adapter:      adapter,
		blockRepo:    blockRepo,
		txRepo:       txRepo,
		log:          slog.Default().With("component", "rescan", "chain", internalCode),
	}
}

// Run starts the worker loop.
func (w *Worker) Run(ctx context.Context) error {
	w.log.Info("Starting rescan worker")

	for {
		select {
		case <-ctx.Done():
			w.log.Info("Rescan worker stopped")
			return nil
		default:
		}

		// Try to merge ranges periodically
		if err := w.mergeQueueRanges(ctx); err != nil {
			w.log.Warn("Failed to merge ranges", "error", err)
		}

		// Pop next range
		start, end, found, err := w.redis.PopRange(ctx, w.internalCode)
		if err != nil {
			w.log.Error("Failed to pop range", "error", err)
			time.Sleep(w.cfg.EmptySleep)
			continue
		}

		if !found {
			// Queue empty, sleep
			time.Sleep(w.cfg.EmptySleep)
			continue
		}

		// Process the range
		if err := w.processRange(ctx, start, end); err != nil {
			w.log.Error("Failed to process range", "start", start, "end", end, "error", err)
			// Re-queue the range for retry
			if requeueErr := w.redis.PushRange(ctx, w.internalCode, start, end); requeueErr != nil {
				w.log.Error("Failed to re-queue range", "error", requeueErr)
			}
		}
	}
}

// processRange processes a single block range.
func (w *Worker) processRange(ctx context.Context, start, end uint64) error {
	w.log.Info("Processing range", "start", start, "end", end)

	// Split into chunks if too large
	fullRange := Range{Start: start, End: end}
	chunks := fullRange.Split(w.cfg.ChunkSize)

	for _, chunk := range chunks {
		if err := w.processChunk(ctx, chunk.Start, chunk.End); err != nil {
			// Re-queue remaining chunks
			for i := len(chunks) - 1; i >= 0; i-- {
				if chunks[i].Start >= chunk.Start {
					if reqErr := w.redis.PushRange(ctx, w.internalCode, chunks[i].Start, chunks[i].End); reqErr != nil {
						w.log.Error("Failed to re-queue chunk", "error", reqErr)
					}
				}
			}
			return err
		}
	}

	// All chunks processed successfully
	w.log.Info("Range completed", "start", start, "end", end)
	return nil
}

// processChunk processes a single chunk with locking.
func (w *Worker) processChunk(ctx context.Context, start, end uint64) error {
	// Acquire lock
	locked, err := w.redis.AcquireLock(ctx, w.internalCode, start, end, w.cfg.LockTTL)
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	if !locked {
		w.log.Debug("Chunk already locked by another worker", "start", start, "end", end)
		return nil // Another worker is processing this
	}

	defer func() {
		if err := w.redis.ReleaseLock(ctx, w.internalCode, start, end); err != nil {
			w.log.Warn("Failed to release lock", "error", err)
		}
		if err := w.redis.ClearProgress(ctx, w.internalCode, start, end); err != nil {
			w.log.Warn("Failed to clear progress", "error", err)
		}
	}()

	// Get progress (in case of resume after crash)
	current, err := w.redis.GetProgress(ctx, w.internalCode, start, end)
	if err != nil {
		return fmt.Errorf("failed to get progress: %w", err)
	}

	w.log.Info("Processing chunk", "start", start, "end", end, "resumeFrom", current)

	// Process each block
	for blockNum := current; blockNum <= end; blockNum++ {
		select {
		case <-ctx.Done():
			// Re-queue remaining
			if err := w.redis.PushRange(ctx, w.internalCode, blockNum, end); err != nil {
				w.log.Error("Failed to re-queue remaining", "error", err)
			}
			return ctx.Err()
		default:
		}

		// Fetch and process block
		if err := w.processBlock(ctx, blockNum); err != nil {
			w.log.Error("Failed to process block", "block", blockNum, "error", err)
			// Re-queue remaining
			if err := w.redis.PushRange(ctx, w.internalCode, blockNum, end); err != nil {
				w.log.Error("Failed to re-queue remaining", "error", err)
			}
			return err
		}

		// Update progress
		if err := w.redis.SetProgress(ctx, w.internalCode, start, end, blockNum+1, w.cfg.ProgressTTL); err != nil {
			w.log.Warn("Failed to update progress", "error", err)
		}

		// Refresh lock periodically
		if blockNum%10 == 0 {
			if err := w.redis.RefreshLock(ctx, w.internalCode, start, end, w.cfg.LockTTL); err != nil {
				w.log.Warn("Failed to refresh lock", "error", err)
			}
		}
	}

	return nil
}

// processBlock fetches and stores a single block.
func (w *Worker) processBlock(ctx context.Context, blockNum uint64) error {
	// Fetch block
	block, err := w.adapter.GetBlock(ctx, blockNum)
	if err != nil {
		return fmt.Errorf("failed to get block %d: %w", blockNum, err)
	}
	if block == nil {
		return fmt.Errorf("block %d not found", blockNum)
	}

	// Fetch transactions
	txs, err := w.adapter.GetTransactions(ctx, block)
	if err != nil {
		return fmt.Errorf("failed to get transactions for block %d: %w", blockNum, err)
	}

	// Save block
	if err := w.blockRepo.Save(ctx, block); err != nil {
		return fmt.Errorf("failed to save block %d: %w", blockNum, err)
	}

	// Save transactions
	if len(txs) > 0 {
		for _, tx := range txs {
			tx.ChainID = block.ChainID
		}
		if err := w.txRepo.SaveBatch(ctx, txs); err != nil {
			w.log.Warn("Failed to save transactions batch", "error", err)
		}
	}

	w.log.Debug("Block processed", "block", blockNum, "txs", len(txs))
	return nil
}

// mergeQueueRanges merges overlapping/adjacent ranges in the queue.
func (w *Worker) mergeQueueRanges(ctx context.Context) error {
	// Get all ranges
	rangeStrs, err := w.redis.GetAllRanges(ctx, w.internalCode)
	if err != nil {
		return err
	}

	if len(rangeStrs) <= 1 {
		return nil // Nothing to merge
	}

	// Parse ranges
	ranges, err := RangesFromStrings(rangeStrs)
	if err != nil {
		return err
	}

	// Merge
	merged := MergeRanges(ranges)

	if len(merged) == len(ranges) {
		return nil // No change
	}

	w.log.Info("Merging ranges", "before", len(ranges), "after", len(merged))

	// Clear and re-add
	if err := w.redis.ClearQueue(ctx, w.internalCode); err != nil {
		return err
	}

	for _, r := range merged {
		if err := w.redis.PushRange(ctx, w.internalCode, r.Start, r.End); err != nil {
			return err
		}
	}

	return nil
}
