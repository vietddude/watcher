package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/storage"
)

// BlockRepo implements storage.BlockRepository using PostgreSQL.
type BlockRepo struct {
	db *DB
}

// NewBlockRepo creates a new PostgreSQL block repository.
func NewBlockRepo(db *DB) *BlockRepo {
	return &BlockRepo{db: db}
}

// Save saves a block to the database.
func (r *BlockRepo) Save(ctx context.Context, block *domain.Block) error {
	query := `
		INSERT INTO blocks (chain_id, block_number, block_hash, parent_hash, block_timestamp, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (chain_id, block_number) DO UPDATE SET
			block_hash = EXCLUDED.block_hash,
			parent_hash = EXCLUDED.parent_hash,
			block_timestamp = EXCLUDED.block_timestamp,
			status = EXCLUDED.status
	`

	_, err := r.db.ExecContext(ctx, query,
		block.ChainID,
		block.Number,
		block.Hash,
		block.ParentHash,
		block.Timestamp,
		string(block.Status),
	)
	if err != nil {
		return fmt.Errorf("failed to save block: %w", err)
	}
	return nil
}

// SaveBatch saves multiple blocks to the database.
func (r *BlockRepo) SaveBatch(ctx context.Context, blocks []*domain.Block) error {
	if len(blocks) == 0 {
		return nil
	}

	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
		INSERT INTO blocks (chain_id, block_number, block_hash, parent_hash, block_timestamp, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (chain_id, block_number) DO UPDATE SET
			block_hash = EXCLUDED.block_hash,
			parent_hash = EXCLUDED.parent_hash,
			block_timestamp = EXCLUDED.block_timestamp,
			status = EXCLUDED.status
	`

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, block := range blocks {
		_, err := stmt.ExecContext(ctx,
			block.ChainID,
			block.Number,
			block.Hash,
			block.ParentHash,
			block.Timestamp,
			string(block.Status),
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

type blockRow struct {
	ChainID    string    `db:"chain_id"`
	Number     uint64    `db:"block_number"`
	Hash       string    `db:"block_hash"`
	ParentHash string    `db:"parent_hash"`
	Timestamp  time.Time `db:"block_timestamp"`
	Status     string    `db:"status"`
}

func (b *blockRow) toDomain() *domain.Block {
	return &domain.Block{
		ChainID:    b.ChainID,
		Number:     b.Number,
		Hash:       b.Hash,
		ParentHash: b.ParentHash,
		Timestamp:  b.Timestamp,
		Status:     domain.BlockStatus(b.Status),
	}
}

// GetByNumber retrieves a block by number.
func (r *BlockRepo) GetByNumber(
	ctx context.Context,
	chainID string,
	blockNumber uint64,
) (*domain.Block, error) {
	query := `
		SELECT chain_id, block_number, block_hash, parent_hash, block_timestamp, status
		FROM blocks
		WHERE chain_id = $1 AND block_number = $2
	`

	var row blockRow
	err := r.db.GetContext(ctx, &row, query, chainID, blockNumber)
	if err == sql.ErrNoRows {
		return nil, nil // Not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	return row.toDomain(), nil
}

// GetByHash retrieves a block by hash.
func (r *BlockRepo) GetByHash(
	ctx context.Context,
	chainID string,
	blockHash string,
) (*domain.Block, error) {
	query := `
		SELECT chain_id, block_number, block_hash, parent_hash, block_timestamp, status
		FROM blocks
		WHERE chain_id = $1 AND block_hash = $2
	`

	var row blockRow
	err := r.db.GetContext(ctx, &row, query, chainID, blockHash)
	if err == sql.ErrNoRows {
		return nil, nil // Not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	return row.toDomain(), nil
}

// GetLatest retrieves the latest indexed block.
func (r *BlockRepo) GetLatest(ctx context.Context, chainID string) (*domain.Block, error) {
	query := `
		SELECT chain_id, block_number, block_hash, parent_hash, block_timestamp, status
		FROM blocks
		WHERE chain_id = $1
		ORDER BY block_number DESC
		LIMIT 1
	`

	var row blockRow
	err := r.db.GetContext(ctx, &row, query, chainID)
	if err == sql.ErrNoRows {
		return nil, nil // Not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get latest block: %w", err)
	}

	return row.toDomain(), nil
}

// UpdateStatus updates block status.
func (r *BlockRepo) UpdateStatus(
	ctx context.Context,
	chainID string,
	blockNumber uint64,
	status domain.BlockStatus,
) error {
	query := `UPDATE blocks SET status = $1 WHERE chain_id = $2 AND block_number = $3`
	_, err := r.db.ExecContext(ctx, query, string(status), chainID, blockNumber)
	if err != nil {
		return fmt.Errorf("failed to update block status: %w", err)
	}
	return nil
}

// FindGaps finds missing blocks in a range.
func (r *BlockRepo) FindGaps(
	ctx context.Context,
	chainID string,
	fromBlock, toBlock uint64,
) ([]storage.Gap, error) {
	query := `
		WITH numbered AS (
			SELECT block_number, LEAD(block_number) OVER (ORDER BY block_number) as next_block
			FROM blocks WHERE chain_id = $1 AND block_number BETWEEN $2 AND $3
		)
		SELECT block_number + 1 as from_block, next_block - 1 as to_block 
		FROM numbered WHERE next_block - block_number > 1
	`

	// Requires struct tag mapping or Scan
	rows, err := r.db.QueryxContext(ctx, query, chainID, fromBlock, toBlock)
	if err != nil {
		return nil, fmt.Errorf("failed to find gaps: %w", err)
	}
	defer rows.Close()

	var gaps []storage.Gap
	for rows.Next() {
		var gap struct {
			FromBlock uint64 `db:"from_block"`
			ToBlock   uint64 `db:"to_block"`
		}
		if err := rows.StructScan(&gap); err != nil {
			return nil, err
		}
		gaps = append(gaps, storage.Gap{FromBlock: gap.FromBlock, ToBlock: gap.ToBlock})
	}
	return gaps, nil
}

// DeleteRange deletes blocks in a range.
func (r *BlockRepo) DeleteRange(
	ctx context.Context,
	chainID string,
	fromBlock, toBlock uint64,
) error {
	query := `DELETE FROM blocks WHERE chain_id = $1 AND block_number BETWEEN $2 AND $3`
	_, err := r.db.ExecContext(ctx, query, chainID, fromBlock, toBlock)
	if err != nil {
		return fmt.Errorf("failed to delete blocks: %w", err)
	}
	return nil
}
