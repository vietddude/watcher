package reorg

import (
	"context"
	"fmt"
	"time"

	"github.com/vietddude/watcher/internal/core/cursor"
	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/storage"
)

// Handler executes reorg rollback operations.
type Handler struct {
	blockRepo storage.BlockRepository
	txRepo    storage.TransactionRepository
	cursorMgr cursor.Manager
	callback  func(event RevertEvent)
}

// RollbackResult contains the result of a rollback operation.
type RollbackResult struct {
	ChainID        string
	OrphanedBlocks int
	RevertedTxs    int
	FromBlock      uint64
	SafeBlock      uint64
	Duration       time.Duration
}

// RevertEvent is emitted for each reverted transaction.
type RevertEvent struct {
	ChainID       string
	TxHash        string
	BlockNumber   uint64
	ReorgDetected time.Time
	Reason        string
}

// SetRevertCallback sets a callback for revert events.
func (h *Handler) SetRevertCallback(fn func(event RevertEvent)) {
	h.callback = fn
}

// Rollback executes the reorg rollback process:
// 1. Mark orphaned blocks
// 2. Mark reverted transactions
// 3. Emit revert events
// 4. Reset cursor
func (h *Handler) Rollback(
	ctx context.Context,
	chainID string,
	fromBlock, safeBlock uint64,
	safeHash string,
) (*RollbackResult, error) {
	start := time.Now()
	result := &RollbackResult{
		ChainID:   chainID,
		FromBlock: fromBlock,
		SafeBlock: safeBlock,
	}

	// Step 1: Mark blocks as orphaned
	for blockNum := fromBlock; blockNum > safeBlock; blockNum-- {
		if err := h.blockRepo.UpdateStatus(ctx, chainID, blockNum, domain.BlockStatusOrphaned); err != nil {
			return nil, fmt.Errorf("failed to mark block %d as orphaned: %w", blockNum, err)
		}
		result.OrphanedBlocks++

		// Step 2: Get and revert transactions in this block
		txs, err := h.txRepo.GetByBlock(ctx, chainID, blockNum)
		if err != nil {
			return nil, fmt.Errorf("failed to get txs for block %d: %w", blockNum, err)
		}

		for _, tx := range txs {
			// Mark transaction as invalid
			if err := h.txRepo.UpdateStatus(ctx, chainID, tx.TxHash, domain.TxStatusInvalid); err != nil {
				return nil, fmt.Errorf("failed to mark tx %s as invalid: %w", tx.TxHash, err)
			}
			result.RevertedTxs++

			// Step 3: Emit revert event
			if h.callback != nil {
				h.callback(RevertEvent{
					ChainID:       chainID,
					TxHash:        tx.TxHash,
					BlockNumber:   blockNum,
					ReorgDetected: time.Now(),
					Reason:        "blockchain_reorganization",
				})
			}
		}
	}

	// Step 4: Rollback cursor
	if err := h.cursorMgr.Rollback(ctx, chainID, safeBlock, safeHash); err != nil {
		return nil, fmt.Errorf("failed to rollback cursor: %w", err)
	}

	result.Duration = time.Since(start)
	return result, nil
}

// CanRecover checks if recovery is possible (safe block exists).
func (h *Handler) CanRecover(ctx context.Context, chainID string, safeBlock uint64) (bool, error) {
	block, err := h.blockRepo.GetByNumber(ctx, chainID, safeBlock)
	if err != nil {
		return false, err
	}
	return block != nil, nil
}
