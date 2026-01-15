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
type BackfillFetcher func(ctx context.Context, chainID string, blockNum uint64) error

// Handler processes the failed block queue.
type Handler struct {
	repo     storage.FailedBlockRepository
	fetcher  BackfillFetcher
	strategy RetryStrategy
}

// NewHandler creates a new failed block handler.
func NewHandler(repo storage.FailedBlockRepository, fetcher BackfillFetcher, strategy RetryStrategy) *Handler {
	return &Handler{
		repo:     repo,
		fetcher:  fetcher,
		strategy: strategy,
	}
}

// ProcessNext picks the next failed block and attempts to retry it.
func (h *Handler) ProcessNext(ctx context.Context, chainID string) error {
	failedBlock, err := h.repo.GetNext(ctx, chainID)
	if err != nil {
		return fmt.Errorf("failed to get next failed block: %w", err)
	}
	if failedBlock == nil {
		return nil // Queue empty
	}

	// Calculate if it's time to retry
	delay := h.strategy.GetDelay(failedBlock.RetryCount)
	nextAttempt := failedBlock.LastAttempt.Add(delay)

	if time.Now().Before(nextAttempt) {
		return nil // Too early to retry
	}

	// Attempt retry
	err = h.fetcher(ctx, chainID, failedBlock.BlockNumber)
	if err == nil {
		// Success! Resolve the failure
		if err := h.repo.MarkResolved(ctx, failedBlock.ID); err != nil {
			return fmt.Errorf("failed to resolve block %s: %w", failedBlock.ID, err)
		}
		return nil
	}

	// Failed again
	// We could check if the error is permanent here using:
	// if s, ok := h.strategy.(*ExponentialBackoff); ok && s.Classifier != nil {
	//     category = s.Classifier(err)
	// }
	// But currently FailedBlockRepository doesn't support updating failure type on retry.

	// If permanent or max retries exceeded -> Human review needed
	// BUT for now, we just increment retry count.
	// In a real system, we might move to a "dead letter" status if permanent.

	if !h.strategy.ShouldRetry(err, failedBlock.RetryCount) {
		// Max attempts reached or permanent error
		// We could mark as "permanently failed" here if the domain supported it,
		// but typically we just leave it with high retry count for admin review.
	}

	if err := h.repo.IncrementRetry(ctx, failedBlock.ID); err != nil {
		return fmt.Errorf("failed to increment retry: %w", err)
	}

	return nil
}

// HandleFailure is called by the main indexer loop when a block fails.
// It creates a new FailedBlock entry.
func (h *Handler) HandleFailure(ctx context.Context, chainID string, blockNum uint64, err error) error {
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
		LastAttempt: time.Now(),
		CreatedAt:   time.Now(),
	}

	if err := h.repo.Add(ctx, failedBlock); err != nil {
		return fmt.Errorf("failed to add failed block: %w", err)
	}
	return nil
}
