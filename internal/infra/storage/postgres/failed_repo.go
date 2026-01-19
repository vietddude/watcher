package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
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
	query := `
		INSERT INTO failed_blocks (chain_id, block_number, error_msg, retry_count, status, last_attempt, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
	`
	// Handle zero retry count if new
	status := string(fb.Status)
	if status == "" {
		status = "pending"
	}

	_, err := r.db.ExecContext(
		ctx,
		query,
		fb.ChainID,
		fb.BlockNumber,
		fb.Error,
		fb.RetryCount,
		status,
	)
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
	query := `
		SELECT id, chain_id, block_number, error_msg, retry_count, last_attempt, status
		FROM failed_blocks
		WHERE chain_id = $1 AND status = 'pending'
		ORDER BY last_attempt ASC
		LIMIT 1
	`

	var dest struct {
		ID          string    `db:"id"`
		ChainID     string    `db:"chain_id"`
		BlockNumber uint64    `db:"block_number"`
		ErrorMsg    string    `db:"error_msg"`
		RetryCount  int       `db:"retry_count"`
		LastAttempt time.Time `db:"last_attempt"`
		Status      string    `db:"status"`
	}

	err := r.db.GetContext(ctx, &dest, query, chainID)
	if err == sql.ErrNoRows {
		return nil, nil // No pending failed blocks
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get failed block: %w", err)
	}

	return &domain.FailedBlock{
		ID:          dest.ID,
		ChainID:     dest.ChainID,
		BlockNumber: dest.BlockNumber,
		Error:       dest.ErrorMsg,
		RetryCount:  dest.RetryCount,
		LastAttempt: dest.LastAttempt,
		Status:      domain.FailedBlockStatus(dest.Status),
	}, nil
}

// IncrementRetry increments retry count and updates timestamp.
func (r *FailedBlockRepo) IncrementRetry(ctx context.Context, id string) error {
	query := `
		UPDATE failed_blocks 
		SET retry_count = retry_count + 1, last_attempt = NOW() 
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

// MarkResolved marks a failed block as resolved.
func (r *FailedBlockRepo) MarkResolved(ctx context.Context, id string) error {
	query := `
		UPDATE failed_blocks 
		SET status = 'resolved' 
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

// GetAll returns all failed blocks (for debugging/monitoring).
func (r *FailedBlockRepo) GetAll(
	ctx context.Context,
	chainID string,
) ([]*domain.FailedBlock, error) {
	query := `
		SELECT id, chain_id, block_number, error_msg, retry_count, last_attempt, status
		FROM failed_blocks
		WHERE chain_id = $1 AND status = 'pending'
	`

	var rows []struct {
		ID          string    `db:"id"`
		ChainID     string    `db:"chain_id"`
		BlockNumber uint64    `db:"block_number"`
		ErrorMsg    string    `db:"error_msg"`
		RetryCount  int       `db:"retry_count"`
		LastAttempt time.Time `db:"last_attempt"`
		Status      string    `db:"status"`
	}

	err := r.db.SelectContext(ctx, &rows, query, chainID)
	if err != nil {
		return nil, fmt.Errorf("failed to get all failed blocks: %w", err)
	}

	var blocks []*domain.FailedBlock
	for _, row := range rows {
		blocks = append(blocks, &domain.FailedBlock{
			ID:          row.ID,
			ChainID:     row.ChainID,
			BlockNumber: row.BlockNumber,
			Error:       row.ErrorMsg,
			RetryCount:  row.RetryCount,
			LastAttempt: row.LastAttempt,
			Status:      domain.FailedBlockStatus(row.Status),
		})
	}
	return blocks, nil
}

// Count returns the number of failed blocks.
func (r *FailedBlockRepo) Count(ctx context.Context, chainID string) (int, error) {
	query := `
		SELECT COUNT(*) 
		FROM failed_blocks 
		WHERE chain_id = $1 AND status = 'pending'
	`
	var count int
	err := r.db.GetContext(ctx, &count, query, chainID)
	if err != nil {
		return 0, fmt.Errorf("failed to count failed blocks: %w", err)
	}
	return count, nil
}
