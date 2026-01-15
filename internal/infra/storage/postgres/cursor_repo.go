package postgres

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/storage"
)

type CursorRepo struct {
	db *sql.DB
}

func NewCursorRepo(db *PostgresDB) *CursorRepo {
	return &CursorRepo{db: db.DB}
}

func (r *CursorRepo) Get(ctx context.Context, chainID string) (*domain.Cursor, error) {
	query := `
		SELECT current_block, current_block_hash, state, metadata, updated_at 
		FROM cursors WHERE chain_id = $1
	`
	row := r.db.QueryRowContext(ctx, query, chainID)

	var c domain.Cursor
	c.ChainID = chainID
	var state string
	var metadata []byte

	err := row.Scan(&c.CurrentBlock, &c.CurrentBlockHash, &state, &metadata, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, storage.ErrCursorNotFound
	}
	if err != nil {
		return nil, err
	}

	c.State = domain.CursorState(state)
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &c.Metadata)
	}

	return &c, nil
}

func (r *CursorRepo) Save(ctx context.Context, cursor *domain.Cursor) error {
	query := `
		INSERT INTO cursors (chain_id, current_block, current_block_hash, state, metadata, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (chain_id) DO UPDATE SET
			current_block = EXCLUDED.current_block,
			current_block_hash = EXCLUDED.current_block_hash,
			state = EXCLUDED.state,
			metadata = EXCLUDED.metadata,
			updated_at = NOW();
	`
	metadata, _ := json.Marshal(cursor.Metadata)

	_, err := r.db.ExecContext(ctx, query,
		cursor.ChainID, cursor.CurrentBlock, cursor.CurrentBlockHash,
		cursor.State, metadata,
	)
	return err
}

func (r *CursorRepo) UpdateBlock(ctx context.Context, chainID string, num uint64, hash string) error {
	query := `UPDATE cursors SET current_block = $1, current_block_hash = $2, updated_at = NOW() WHERE chain_id = $3`
	result, err := r.db.ExecContext(ctx, query, num, hash, chainID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return storage.ErrCursorNotFound
	}
	return nil
}

func (r *CursorRepo) UpdateState(ctx context.Context, chainID string, state domain.CursorState) error {
	query := `UPDATE cursors SET state = $1, updated_at = NOW() WHERE chain_id = $2`
	result, err := r.db.ExecContext(ctx, query, state, chainID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return storage.ErrCursorNotFound
	}
	return nil
}

func (r *CursorRepo) Rollback(ctx context.Context, chainID string, num uint64, hash string) error {
	return r.UpdateBlock(ctx, chainID, num, hash)
}
