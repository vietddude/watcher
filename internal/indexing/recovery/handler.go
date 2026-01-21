package recovery

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/storage"
)

// BackfillFetcher is a callback to retry processing a block.
type BackfillFetcher func(ctx context.Context, chainID domain.ChainID, blockNum uint64) error

// Handler processes the failed block queue.
type Handler struct {
	repo     storage.FailedBlockRepository
	fetcher  BackfillFetcher
	strategy RetryStrategy
}

// NewHandler creates a new failed block handler.
func NewHandler(
	repo storage.FailedBlockRepository,
	fetcher BackfillFetcher,
	strategy RetryStrategy,
) *Handler {
	return &Handler{
		repo:     repo,
		fetcher:  fetcher,
		strategy: strategy,
	}
}

// ProcessNext picks the next failed block and retries it if backoff allows.
func (h *Handler) ProcessNext(ctx context.Context, chainID domain.ChainID) error {
	failedBlock, err := h.repo.GetNext(ctx, chainID)
	if err != nil {
		return fmt.Errorf("failed to get next failed block: %w", err)
	}
	if failedBlock == nil {
		return nil
	}

	delay := h.strategy.GetDelay(failedBlock.RetryCount)
	lastAttempt := time.Unix(int64(failedBlock.LastAttempt), 0)
	if time.Now().Before(lastAttempt.Add(delay)) {
		return nil
	}

	if err := h.fetcher(ctx, chainID, failedBlock.BlockNumber); err == nil {
		if err := h.repo.MarkResolved(ctx, failedBlock.ID); err != nil {
			return fmt.Errorf("failed to resolve block %s: %w", failedBlock.ID, err)
		}
		return nil
	}

	if err := h.repo.IncrementRetry(ctx, failedBlock.ID); err != nil {
		return fmt.Errorf("failed to increment retry: %w", err)
	}

	return nil
}

// HandleFailure is called by the main indexer loop when a block fails.
// It creates a new FailedBlock entry.
func (h *Handler) HandleFailure(
	ctx context.Context,
	chainID domain.ChainID,
	blockNum uint64,
	err error,
) error {
	// Determine failure type string from error
	failureType := domain.FailureTypeRPC // Default
	// simplified logic, could be more complex mapping

	failedBlock := &domain.FailedBlock{
		ID:          uuid.New().String(),
		ChainID:     chainID,
		BlockNumber: blockNum,
		FailureType: failureType,
		Error:       err.Error(),
		RetryCount:  0,
		Status:      domain.FailedBlockStatusPending,
		LastAttempt: uint64(time.Now().Unix()),
		CreatedAt:   uint64(time.Now().Unix()),
	}

	if err := h.repo.Add(ctx, failedBlock); err != nil {
		return fmt.Errorf("failed to add failed block: %w", err)
	}
	return nil
}
