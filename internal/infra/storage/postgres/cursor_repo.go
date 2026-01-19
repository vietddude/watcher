package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/storage/postgres/sqlc"
)

// CursorRepo implements storage.CursorRepository using PostgreSQL.
type CursorRepo struct {
	db *DB
}

// NewCursorRepo creates a new PostgreSQL cursor repository.
func NewCursorRepo(db *DB) *CursorRepo {
	return &CursorRepo{db: db}
}

// Save saves a cursor to the database.
func (r *CursorRepo) Save(ctx context.Context, cursor *domain.Cursor) error {
	err := r.db.Queries.UpsertCursor(ctx, sqlc.UpsertCursorParams{
		ChainID:     cursor.ChainID,
		BlockNumber: int64(cursor.CurrentBlock),
		BlockHash:   cursor.CurrentBlockHash,
		State:       string(cursor.State),
		UpdatedAt:   sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to save cursor: %w", err)
	}
	return nil
}

// Get retrieves a cursor by chain ID.
func (r *CursorRepo) Get(ctx context.Context, chainID string) (*domain.Cursor, error) {
	row, err := r.db.Queries.GetCursor(ctx, chainID)
	if err == sql.ErrNoRows {
		return nil, nil // Not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get cursor: %w", err)
	}

	return &domain.Cursor{
		ChainID:          row.ChainID,
		CurrentBlock:     uint64(row.BlockNumber),
		CurrentBlockHash: row.BlockHash,
		State:            domain.CursorState(row.State),
		UpdatedAt:        uint64(row.UpdatedAt.Int64),
	}, nil
}

// UpdateBlock updates cursor to a new block (atomic operation).
func (r *CursorRepo) UpdateBlock(
	ctx context.Context,
	chainID string,
	blockNumber uint64,
	blockHash string,
) error {
	// UpsertCursorBlock handles insert if not exists (with default state 'running')
	// or update if exists (preserving state).
	err := r.db.Queries.UpsertCursorBlock(ctx, sqlc.UpsertCursorBlockParams{
		ChainID:     chainID,
		BlockNumber: int64(blockNumber),
		BlockHash:   blockHash,
		UpdatedAt:   sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to update cursor block: %w", err)
	}
	return nil
}

// UpdateState updates cursor state.
func (r *CursorRepo) UpdateState(
	ctx context.Context,
	chainID string,
	state domain.CursorState,
) error {
	err := r.db.Queries.UpdateCursorState(ctx, sqlc.UpdateCursorStateParams{
		State:     string(state),
		ChainID:   chainID,
		UpdatedAt: sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
	})
	return err
}

// Rollback rolls back cursor to a previous block.
func (r *CursorRepo) Rollback(
	ctx context.Context,
	chainID string,
	blockNumber uint64,
	blockHash string,
) error {
	// Rollback behaves like UpdateBlock
	return r.UpdateBlock(ctx, chainID, blockNumber, blockHash)
}
