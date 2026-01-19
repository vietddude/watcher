package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
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
	query := `
		INSERT INTO cursors (chain_id, block_number, block_hash, state, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (chain_id) DO UPDATE SET
			block_number = EXCLUDED.block_number,
			block_hash = EXCLUDED.block_hash,
			updated_at = NOW()
	`
	state := string(cursor.State)
	if state == "" {
		state = "running"
	}

	_, err := r.db.ExecContext(ctx, query,
		cursor.ChainID,
		cursor.CurrentBlock,
		cursor.CurrentBlockHash,
		state,
	)
	if err != nil {
		return fmt.Errorf("failed to save cursor: %w", err)
	}
	return nil
}

// Get retrieves a cursor by chain ID.
func (r *CursorRepo) Get(ctx context.Context, chainID string) (*domain.Cursor, error) {
	query := `
		SELECT chain_id, block_number, block_hash, state, updated_at
		FROM cursors
		WHERE chain_id = $1
	`

	var dest struct {
		ChainID     string    `db:"chain_id"`
		BlockNumber uint64    `db:"block_number"`
		BlockHash   string    `db:"block_hash"`
		State       string    `db:"state"`
		UpdatedAt   time.Time `db:"updated_at"`
	}

	err := r.db.GetContext(ctx, &dest, query, chainID)
	if err == sql.ErrNoRows {
		return nil, nil // Not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get cursor: %w", err)
	}

	return &domain.Cursor{
		ChainID:          dest.ChainID,
		CurrentBlock:     dest.BlockNumber,
		CurrentBlockHash: dest.BlockHash,
		State:            domain.CursorState(dest.State),
		UpdatedAt:        dest.UpdatedAt,
	}, nil
}

// UpdateBlock updates cursor to a new block (atomic operation).
func (r *CursorRepo) UpdateBlock(
	ctx context.Context,
	chainID string,
	blockNumber uint64,
	blockHash string,
) error {
	query := `
		UPDATE cursors 
		SET block_number = $1, block_hash = $2, updated_at = NOW()
		WHERE chain_id = $3
	`
	res, err := r.db.ExecContext(ctx, query, blockNumber, blockHash, chainID)
	if err != nil {
		return fmt.Errorf("failed to update cursor block: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		// Attempt insert if not exists (upsert logic usually handled by Save, but here strictly update)
		// Or should we UPSERT? Interface says UpdateBlock, implies existing cursor?
		// Memory repo might upsert. Let's assume cursor exists or use UPSERT logic if safe.
		// For consistency, let's use UPSERT similar to Save but only updating block fields.
		upsertQuery := `
			INSERT INTO cursors (chain_id, block_number, block_hash, state, updated_at)
			VALUES ($1, $2, $3, 'running', NOW())
			ON CONFLICT (chain_id) DO UPDATE SET
				block_number = EXCLUDED.block_number,
				block_hash = EXCLUDED.block_hash,
				updated_at = NOW()
		`
		_, err := r.db.ExecContext(ctx, upsertQuery, chainID, blockNumber, blockHash)
		return err
	}
	return nil
}

// UpdateState updates cursor state.
func (r *CursorRepo) UpdateState(
	ctx context.Context,
	chainID string,
	state domain.CursorState,
) error {
	query := `
		UPDATE cursors 
		SET state = $1, updated_at = NOW()
		WHERE chain_id = $2
	`
	_, err := r.db.ExecContext(ctx, query, string(state), chainID)
	return err
}

// Rollback rolls back cursor to a previous block.
func (r *CursorRepo) Rollback(
	ctx context.Context,
	chainID string,
	blockNumber uint64,
	blockHash string,
) error {
	// Same as UpdateBlock basically, maybe update state to 'scanning' if it was something else?
	return r.UpdateBlock(ctx, chainID, blockNumber, blockHash)
}
