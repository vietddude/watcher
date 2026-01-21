package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/storage/postgres/sqlc"
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
	status := string(missing.Status)
	if status == "" {
		status = "pending"
	}

	err := r.db.Queries.CreateMissingBlock(ctx, sqlc.CreateMissingBlockParams{
		ChainID:   string(missing.ChainID),
		FromBlock: int64(missing.FromBlock),
		ToBlock:   int64(missing.ToBlock),
		Status:    status,
	})
	if err != nil {
		return fmt.Errorf("failed to add missing range: %w", err)
	}
	return nil
}

// GetNext retrieves the next missing block to process.
func (r *MissingBlockRepo) GetNext(
	ctx context.Context,
	chainID domain.ChainID,
) (*domain.MissingBlock, error) {
	row, err := r.db.Queries.GetNextMissingBlock(ctx, string(chainID))
	if err == sql.ErrNoRows {
		return nil, nil // Assuming no rows means no work
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get next missing block: %w", err)
	}

	return &domain.MissingBlock{
		ID:        fmt.Sprintf("%d", row.ID), // ID is int32 in DB/sqlc, string in domain?
		ChainID:   domain.ChainID(row.ChainID),
		FromBlock: uint64(row.FromBlock),
		ToBlock:   uint64(row.ToBlock),
		Status:    domain.MissingBlockStatus(row.Status),
	}, nil
}

// MarkProcessing marks a range as being processed.
func (r *MissingBlockRepo) MarkProcessing(ctx context.Context, id string) error {
	// ID is string in domain, int32 in DB/sqlc. Need conversion.
	var intID int32
	_, err := fmt.Sscanf(id, "%d", &intID)
	if err != nil {
		return fmt.Errorf("invalid id format: %w", err)
	}
	err = r.db.Queries.MarkMissingBlockProcessing(ctx, intID)
	return err
}

// MarkCompleted marks a range as completed.
func (r *MissingBlockRepo) MarkCompleted(ctx context.Context, id string) error {
	var intID int32
	_, err := fmt.Sscanf(id, "%d", &intID)
	if err != nil {
		return fmt.Errorf("invalid id format: %w", err)
	}
	err = r.db.Queries.MarkMissingBlockCompleted(ctx, intID)
	return err
}

// MarkFailed marks a range as failed.
func (r *MissingBlockRepo) MarkFailed(ctx context.Context, id string, errorMsg string) error {
	var intID int32
	_, err := fmt.Sscanf(id, "%d", &intID)
	if err != nil {
		return fmt.Errorf("invalid id format: %w", err)
	}
	err = r.db.Queries.MarkMissingBlockFailed(ctx, sqlc.MarkMissingBlockFailedParams{
		ID:       intID,
		ErrorMsg: sql.NullString{String: errorMsg, Valid: errorMsg != ""},
	})
	return err
}

// GetPending retrieves all pending missing blocks.
func (r *MissingBlockRepo) GetPending(
	ctx context.Context,
	chainID domain.ChainID,
) ([]*domain.MissingBlock, error) {
	rows, err := r.db.Queries.GetPendingMissingBlocks(ctx, string(chainID))
	if err != nil {
		return nil, fmt.Errorf("failed to get pending ranges: %w", err)
	}

	var ranges []*domain.MissingBlock
	for _, row := range rows {
		ranges = append(ranges, &domain.MissingBlock{
			ID:        fmt.Sprintf("%d", row.ID),
			ChainID:   domain.ChainID(row.ChainID),
			FromBlock: uint64(row.FromBlock),
			ToBlock:   uint64(row.ToBlock),
			Status:    domain.MissingBlockStatus(row.Status),
		})
	}
	return ranges, nil
}

// Count returns the count of missing blocks.
func (r *MissingBlockRepo) Count(ctx context.Context, chainID domain.ChainID) (int, error) {
	count, err := r.db.Queries.CountPendingMissingBlocks(ctx, string(chainID))
	if err != nil {
		return 0, fmt.Errorf("failed to count missing ranges: %w", err)
	}
	return int(count), nil
}
