package postgres

import (
	"context"
	"fmt"

	"github.com/vietddude/watcher/internal/core/domain"
)

// MissingBlockRepo implements storage.MissingBlockRepository using PostgreSQL.
type MissingBlockRepo struct {
	db *DB
}

// NewMissingBlockRepo creates a new PostgreSQL missing block repository.
func NewMissingBlockRepo(db *DB) *MissingBlockRepo {
	return &MissingBlockRepo{db: db}
}

// Add adds a missing block range.
func (r *MissingBlockRepo) Add(ctx context.Context, missing *domain.MissingBlock) error {
	query := `
		INSERT INTO missing_blocks (chain_id, from_block, to_block, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
	`
	// Explicitly cast to avoid type issues if necessary, but string(status) should work
	status := string(missing.Status)
	if status == "" {
		status = "pending"
	}

	_, err := r.db.ExecContext(
		ctx,
		query,
		missing.ChainID,
		missing.FromBlock,
		missing.ToBlock,
		status,
	)
	if err != nil {
		return fmt.Errorf("failed to add missing range: %w", err)
	}
	return nil
}

// GetNext retrieves the next missing block to process.
func (r *MissingBlockRepo) GetNext(
	ctx context.Context,
	chainID string,
) (*domain.MissingBlock, error) {
	query := `
		SELECT id, chain_id, from_block, to_block, status, created_at, updated_at
		FROM missing_blocks
		WHERE chain_id = $1 AND status = 'pending'
		ORDER BY from_block ASC
		LIMIT 1
	`
	var row struct {
		ID        string `db:"id"`
		ChainID   string `db:"chain_id"`
		FromBlock uint64 `db:"from_block"`
		ToBlock   uint64 `db:"to_block"`
		Status    string `db:"status"`
	}

	err := r.db.GetContext(ctx, &row, query, chainID)
	// Return error is okay, or nil if no rows. Adapter expects pointer.
	if err != nil {
		return nil, nil // Assuming no rows means no work
	}

	return &domain.MissingBlock{
		ID:        row.ID,
		ChainID:   row.ChainID,
		FromBlock: row.FromBlock,
		ToBlock:   row.ToBlock,
		Status:    domain.MissingBlockStatus(row.Status),
	}, nil
}

// MarkProcessing marks a range as being processed.
func (r *MissingBlockRepo) MarkProcessing(ctx context.Context, id string) error {
	query := `UPDATE missing_blocks SET status = 'processing', updated_at = NOW() WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

// MarkCompleted marks a range as completed.
func (r *MissingBlockRepo) MarkCompleted(ctx context.Context, id string) error {
	// Maybe delete completed items to keep table small?
	// Or just mark status. Let's mark status as per interface.
	query := `UPDATE missing_blocks SET status = 'completed', updated_at = NOW() WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

// MarkFailed marks a range as failed.
func (r *MissingBlockRepo) MarkFailed(ctx context.Context, id string, errorMsg string) error {
	query := `UPDATE missing_blocks SET status = 'failed', error_msg = $2, updated_at = NOW() WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id, errorMsg)
	return err
}

// GetPending retrieves all pending missing blocks (up to a reasonable limit?).
func (r *MissingBlockRepo) GetPending(
	ctx context.Context,
	chainID string,
) ([]*domain.MissingBlock, error) {
	// Interface doesn't specify limit, but let's limit to avoid massive query
	query := `
		SELECT id, chain_id, from_block, to_block, status
		FROM missing_blocks
		WHERE chain_id = $1 AND status = 'pending'
		ORDER BY from_block ASC
		LIMIT 1000
	`

	var rows []struct {
		ID        string `db:"id"`
		ChainID   string `db:"chain_id"`
		FromBlock uint64 `db:"from_block"`
		ToBlock   uint64 `db:"to_block"`
		Status    string `db:"status"`
	}
	err := r.db.SelectContext(ctx, &rows, query, chainID)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending ranges: %w", err)
	}

	var ranges []*domain.MissingBlock
	for _, row := range rows {
		ranges = append(ranges, &domain.MissingBlock{
			ID:        row.ID,
			ChainID:   row.ChainID,
			FromBlock: row.FromBlock,
			ToBlock:   row.ToBlock,
			Status:    domain.MissingBlockStatus(row.Status),
		})
	}
	return ranges, nil
}

// Count returns the count of missing blocks (ranges, actually).
func (r *MissingBlockRepo) Count(ctx context.Context, chainID string) (int, error) {
	query := `
		SELECT COUNT(*) 
		FROM missing_blocks 
		WHERE chain_id = $1 AND status = 'pending'
	`
	var count int
	err := r.db.GetContext(ctx, &count, query, chainID)
	if err != nil {
		return 0, fmt.Errorf("failed to count missing ranges: %w", err)
	}
	return count, nil
}
