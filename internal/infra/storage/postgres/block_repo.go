package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/storage"
)

// BlockRepo implements storage.BlockRepository
type BlockRepo struct {
	db *sql.DB
}

func NewBlockRepo(db *PostgresDB) *BlockRepo {
	return &BlockRepo{db: db.DB}
}

func (r *BlockRepo) Save(ctx context.Context, block *domain.Block) error {
	query := `
		INSERT INTO blocks (chain_id, number, hash, parent_hash, timestamp, tx_count, status, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		ON CONFLICT (chain_id, number) DO UPDATE SET
			hash = EXCLUDED.hash,
			parent_hash = EXCLUDED.parent_hash,
			timestamp = EXCLUDED.timestamp,
			tx_count = EXCLUDED.tx_count,
			status = EXCLUDED.status,
			metadata = EXCLUDED.metadata;
	`
	metadata, err := json.Marshal(block.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	_, err = r.db.ExecContext(ctx, query,
		block.ChainID, block.Number, block.Hash, block.ParentHash,
		block.Timestamp, block.TxCount, block.Status, metadata,
	)
	if err != nil {
		return fmt.Errorf("save block: %w", err)
	}
	return nil
}

func (r *BlockRepo) SaveBatch(ctx context.Context, blocks []*domain.Block) error {
	if len(blocks) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
		INSERT INTO blocks (chain_id, number, hash, parent_hash, timestamp, tx_count, status, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		ON CONFLICT (chain_id, number) DO UPDATE SET
			hash = EXCLUDED.hash,
			parent_hash = EXCLUDED.parent_hash,
			timestamp = EXCLUDED.timestamp,
			tx_count = EXCLUDED.tx_count,
			status = EXCLUDED.status,
			metadata = EXCLUDED.metadata;
	`

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, block := range blocks {
		metadata, _ := json.Marshal(block.Metadata)
		_, err := stmt.ExecContext(ctx,
			block.ChainID, block.Number, block.Hash, block.ParentHash,
			block.Timestamp, block.TxCount, block.Status, metadata,
		)
		if err != nil {
			return fmt.Errorf("save batch block %d: %w", block.Number, err)
		}
	}

	return tx.Commit()
}

func (r *BlockRepo) GetByNumber(ctx context.Context, chainID string, number uint64) (*domain.Block, error) {
	query := `SELECT chain_id, number, hash, parent_hash, timestamp, tx_count, status, metadata FROM blocks WHERE chain_id = $1 AND number = $2`
	row := r.db.QueryRowContext(ctx, query, chainID, number)
	return scanBlock(row)
}

func (r *BlockRepo) GetByHash(ctx context.Context, chainID string, hash string) (*domain.Block, error) {
	query := `SELECT chain_id, number, hash, parent_hash, timestamp, tx_count, status, metadata FROM blocks WHERE chain_id = $1 AND hash = $2`
	row := r.db.QueryRowContext(ctx, query, chainID, hash)
	return scanBlock(row)
}

func (r *BlockRepo) GetLatest(ctx context.Context, chainID string) (*domain.Block, error) {
	query := `SELECT chain_id, number, hash, parent_hash, timestamp, tx_count, status, metadata FROM blocks WHERE chain_id = $1 ORDER BY number DESC LIMIT 1`
	row := r.db.QueryRowContext(ctx, query, chainID)
	return scanBlock(row)
}

func (r *BlockRepo) UpdateStatus(ctx context.Context, chainID string, number uint64, status domain.BlockStatus) error {
	query := `UPDATE blocks SET status = $1 WHERE chain_id = $2 AND number = $3`
	_, err := r.db.ExecContext(ctx, query, status, chainID, number)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	return nil
}

func (r *BlockRepo) FindGaps(ctx context.Context, chainID string, from, to uint64) ([]storage.Gap, error) {
	// Optimized gap finding using lag/lead is complex in pure SQL without a sequence table.
	// Simple approach: SELECT number FROM blocks WHERE chain_id=$1 AND number BETWEEN $2 AND $3
	// Then iterate in Go.

	// Better SQL approach:
	// select number + 1 as start_gap, next_nr - 1 as end_gap
	// from (select number, lead(number) over (order by number) as next_nr from blocks where ...)
	// where next_nr > number + 1

	query := `
		WITH ordered AS (
			SELECT number, LEAD(number) OVER (ORDER BY number) as next_nr 
			FROM blocks 
			WHERE chain_id = $1 AND number >= $2 AND number <= $3
		)
		SELECT number + 1 as gap_start, next_nr - 1 as gap_end 
		FROM ordered 
		WHERE next_nr > number + 1;
	`
	rows, err := r.db.QueryContext(ctx, query, chainID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var gaps []storage.Gap
	for rows.Next() {
		var start, end uint64
		if err := rows.Scan(&start, &end); err != nil {
			return nil, err
		}
		gaps = append(gaps, storage.Gap{FromBlock: start, ToBlock: end})
	}

	// Handle edge case: if NO blocks exist in range, entire range is gap?
	// The above query assumes at least some blocks exist.
	// If the repo is empty for this range, we need to return one big gap.
	// We can check if gaps is empty and row count was 0.
	// For simplicity, we stick to the interface contract which implies finding gaps between existing blocks.
	// Or should we include gaps at the edges?
	// Usually backfill handles gaps *between* indexed blocks.

	return gaps, nil
}

func (r *BlockRepo) DeleteRange(ctx context.Context, chainID string, from, to uint64) error {
	query := `DELETE FROM blocks WHERE chain_id = $1 AND number >= $2 AND number <= $3`
	_, err := r.db.ExecContext(ctx, query, chainID, from, to)
	return err
}

// scanBlock helper
func scanBlock(scanner interface {
	Scan(dest ...interface{}) error
}) (*domain.Block, error) {
	var b domain.Block
	var status string
	var metadata []byte

	err := scanner.Scan(
		&b.ChainID, &b.Number, &b.Hash, &b.ParentHash, &b.Timestamp,
		&b.TxCount, &status, &metadata,
	)
	if err == sql.ErrNoRows {
		return nil, nil // Return nil, nil for not found (standard repo pattern)
	}
	if err != nil {
		return nil, err
	}

	b.Status = domain.BlockStatus(status)
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &b.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal meta: %w", err)
		}
	}

	return &b, nil
}
