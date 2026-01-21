package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/storage"
	"github.com/vietddude/watcher/internal/infra/storage/postgres/sqlc"
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
	err := r.db.Queries.CreateBlock(ctx, sqlc.CreateBlockParams{
		ChainID:        string(block.ChainID),
		BlockNumber:    int64(block.Number),
		BlockHash:      block.Hash,
		ParentHash:     block.ParentHash,
		BlockTimestamp: int64(block.Timestamp),
		Status:         string(block.Status),
	})
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

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	qtx := r.db.Queries.WithTx(tx)

	for _, block := range blocks {
		err := qtx.CreateBlock(ctx, sqlc.CreateBlockParams{
			ChainID:        string(block.ChainID),
			BlockNumber:    int64(block.Number),
			BlockHash:      block.Hash,
			ParentHash:     block.ParentHash,
			BlockTimestamp: int64(block.Timestamp),
			Status:         string(block.Status),
		})
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetByNumber retrieves a block by number.
func (r *BlockRepo) GetByNumber(
	ctx context.Context,
	chainID domain.ChainID,
	blockNumber uint64,
) (*domain.Block, error) {
	row, err := r.db.Queries.GetBlockByNumber(ctx, sqlc.GetBlockByNumberParams{
		ChainID:     string(chainID),
		BlockNumber: int64(blockNumber),
	})
	if err == sql.ErrNoRows {
		return nil, nil // Not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	return &domain.Block{
		ChainID:    domain.ChainID(row.ChainID),
		Number:     uint64(row.BlockNumber),
		Hash:       row.BlockHash,
		ParentHash: row.ParentHash,
		Timestamp:  uint64(row.BlockTimestamp),
		Status:     domain.BlockStatus(row.Status),
	}, nil
}

// GetByHash retrieves a block by hash.
func (r *BlockRepo) GetByHash(
	ctx context.Context,
	chainID domain.ChainID,
	blockHash string,
) (*domain.Block, error) {
	row, err := r.db.Queries.GetBlockByHash(ctx, sqlc.GetBlockByHashParams{
		ChainID:   string(chainID),
		BlockHash: blockHash,
	})
	if err == sql.ErrNoRows {
		return nil, nil // Not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	return &domain.Block{
		ChainID:    domain.ChainID(row.ChainID),
		Number:     uint64(row.BlockNumber),
		Hash:       row.BlockHash,
		ParentHash: row.ParentHash,
		Timestamp:  uint64(row.BlockTimestamp),
		Status:     domain.BlockStatus(row.Status),
	}, nil
}

// GetLatest retrieves the latest indexed block.
func (r *BlockRepo) GetLatest(ctx context.Context, chainID domain.ChainID) (*domain.Block, error) {
	row, err := r.db.Queries.GetLatestBlock(ctx, string(chainID))
	if err == sql.ErrNoRows {
		return nil, nil // Not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get latest block: %w", err)
	}

	return &domain.Block{
		ChainID:    domain.ChainID(row.ChainID),
		Number:     uint64(row.BlockNumber),
		Hash:       row.BlockHash,
		ParentHash: row.ParentHash,
		Timestamp:  uint64(row.BlockTimestamp),
		Status:     domain.BlockStatus(row.Status),
	}, nil
}

// UpdateStatus updates block status.
func (r *BlockRepo) UpdateStatus(
	ctx context.Context,
	chainID domain.ChainID,
	blockNumber uint64,
	status domain.BlockStatus,
) error {
	err := r.db.Queries.UpdateBlockStatus(ctx, sqlc.UpdateBlockStatusParams{
		Status:      string(status),
		ChainID:     string(chainID),
		BlockNumber: int64(blockNumber),
	})
	if err != nil {
		return fmt.Errorf("failed to update block status: %w", err)
	}
	return nil
}

// FindGaps finds missing blocks in a range.
func (r *BlockRepo) FindGaps(
	ctx context.Context,
	chainID domain.ChainID,
	fromBlock, toBlock uint64,
) ([]storage.Gap, error) {
	rows, err := r.db.Queries.FindGaps(ctx, sqlc.FindGapsParams{
		ChainID:       string(chainID),
		BlockNumber:   int64(fromBlock),
		BlockNumber_2: int64(toBlock),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find gaps: %w", err)
	}

	var gaps []storage.Gap
	for _, row := range rows {
		gaps = append(gaps, storage.Gap{
			FromBlock: uint64(row.FromBlock),
			ToBlock:   uint64(row.ToBlock),
		})
	}
	return gaps, nil
}

// DeleteRange deletes blocks in a range.
func (r *BlockRepo) DeleteRange(
	ctx context.Context,
	chainID domain.ChainID,
	fromBlock, toBlock uint64,
) error {
	err := r.db.Queries.DeleteBlocksInRange(ctx, sqlc.DeleteBlocksInRangeParams{
		ChainID:       string(chainID),
		BlockNumber:   int64(fromBlock),
		BlockNumber_2: int64(toBlock),
	})
	if err != nil {
		return fmt.Errorf("failed to delete blocks: %w", err)
	}
	return nil
}

// DeleteBlocksOlderThan deletes blocks older than the given timestamp.
func (r *BlockRepo) DeleteBlocksOlderThan(
	ctx context.Context,
	chainID domain.ChainID,
	timestamp uint64,
) error {
	err := r.db.Queries.DeleteBlocksOlderThan(ctx, sqlc.DeleteBlocksOlderThanParams{
		ChainID:        string(chainID),
		BlockTimestamp: int64(timestamp),
	})
	if err != nil {
		return fmt.Errorf("failed to delete old blocks: %w", err)
	}
	return nil
}
