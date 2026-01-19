package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/storage/postgres/sqlc"
)

// FailedBlockRepo implements storage.FailedBlockRepository using PostgreSQL.
type FailedBlockRepo struct {
	db *DB
}

// NewFailedBlockRepo creates a new PostgreSQL failed block repository.
func NewFailedBlockRepo(db *DB) *FailedBlockRepo {
	return &FailedBlockRepo{db: db}
}

// Add adds a failed block.
func (r *FailedBlockRepo) Add(ctx context.Context, fb *domain.FailedBlock) error {
	status := string(fb.Status)
	if status == "" {
		status = "pending"
	}

	err := r.db.Queries.CreateFailedBlock(ctx, sqlc.CreateFailedBlockParams{
		ChainID:     fb.ChainID,
		BlockNumber: int64(fb.BlockNumber),
		ErrorMsg:    sql.NullString{String: fb.Error, Valid: fb.Error != ""},
		RetryCount:  int32(fb.RetryCount),
		Status:      status,
		LastAttempt: sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
		CreatedAt:   sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to add failed block: %w", err)
	}
	return nil
}

// GetNext returns the next failed block to retry.
func (r *FailedBlockRepo) GetNext(
	ctx context.Context,
	chainID string,
) (*domain.FailedBlock, error) {
	row, err := r.db.Queries.GetNextFailedBlock(ctx, chainID)
	if err == sql.ErrNoRows {
		return nil, nil // No pending failed blocks
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get failed block: %w", err)
	}

	return &domain.FailedBlock{
		ID:          fmt.Sprintf("%d", row.ID),
		ChainID:     row.ChainID,
		BlockNumber: uint64(row.BlockNumber),
		Error:       row.ErrorMsg.String,
		RetryCount:  int(row.RetryCount),
		LastAttempt: uint64(row.LastAttempt.Int64),
		Status:      domain.FailedBlockStatus(row.Status),
	}, nil
}

// IncrementRetry increments retry count and updates timestamp.
func (r *FailedBlockRepo) IncrementRetry(ctx context.Context, id string) error {
	var intID int32
	_, err := fmt.Sscanf(id, "%d", &intID)
	if err != nil {
		return fmt.Errorf("invalid id format: %w", err)
	}
	err = r.db.Queries.IncrementFailedBlockRetry(ctx, sqlc.IncrementFailedBlockRetryParams{
		ID:          int32(intID),
		LastAttempt: sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
	})
	return err
}

// MarkResolved marks a failed block as resolved.
func (r *FailedBlockRepo) MarkResolved(ctx context.Context, id string) error {
	var intID int32
	_, err := fmt.Sscanf(id, "%d", &intID)
	if err != nil {
		return fmt.Errorf("invalid id format: %w", err)
	}
	err = r.db.Queries.MarkFailedBlockResolved(ctx, intID)
	return err
}

// GetAll returns all failed blocks (for debugging/monitoring).
func (r *FailedBlockRepo) GetAll(
	ctx context.Context,
	chainID string,
) ([]*domain.FailedBlock, error) {
	rows, err := r.db.Queries.GetAllFailedBlocks(ctx, chainID)
	if err != nil {
		return nil, fmt.Errorf("failed to get all failed blocks: %w", err)
	}

	var blocks []*domain.FailedBlock
	for _, row := range rows {
		blocks = append(blocks, &domain.FailedBlock{
			ID:          fmt.Sprintf("%d", row.ID),
			ChainID:     row.ChainID,
			BlockNumber: uint64(row.BlockNumber),
			Error:       row.ErrorMsg.String,
			RetryCount:  int(row.RetryCount),
			LastAttempt: uint64(row.LastAttempt.Int64),
			Status:      domain.FailedBlockStatus(row.Status),
		})
	}
	return blocks, nil
}

// Count returns the number of failed blocks.
func (r *FailedBlockRepo) Count(ctx context.Context, chainID string) (int, error) {
	count, err := r.db.Queries.CountFailedBlocks(ctx, chainID)
	if err != nil {
		return 0, fmt.Errorf("failed to count failed blocks: %w", err)
	}
	return int(count), nil
}
