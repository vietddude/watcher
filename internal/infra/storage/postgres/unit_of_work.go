package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/indexing/metrics"
	"github.com/vietddude/watcher/internal/infra/storage/postgres/sqlc"
)

// UnitOfWork bundles all persistence operations into a single database transaction,
// ensuring atomicity (all succeed or all fail).
type UnitOfWork struct {
	db      *DB
	tx      *sql.Tx
	queries *sqlc.Queries
}

// NewUnitOfWork creates a new unit of work with an active transaction.
func (db *DB) NewUnitOfWork(ctx context.Context) (*UnitOfWork, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	return &UnitOfWork{
		db:      db,
		tx:      tx,
		queries: db.Queries.WithTx(tx),
	}, nil
}

// Commit commits the transaction.
func (u *UnitOfWork) Commit() error {
	if u.tx == nil {
		return fmt.Errorf("transaction already completed")
	}
	err := u.tx.Commit()
	u.tx = nil
	return err
}

// Rollback rolls back the transaction. Safe to call multiple times.
func (u *UnitOfWork) Rollback() error {
	if u.tx == nil {
		return nil // Already committed or rolled back
	}
	err := u.tx.Rollback()
	u.tx = nil
	return err
}

// SaveBlocks saves multiple blocks in the transaction using multi-row INSERT.
func (u *UnitOfWork) SaveBlocks(ctx context.Context, blocks []*domain.Block) error {
	if len(blocks) == 0 {
		return nil
	}

	chainIDs := make([]string, len(blocks))
	blockNumbers := make([]int64, len(blocks))
	blockHashes := make([]string, len(blocks))
	parentHashes := make([]string, len(blocks))
	blockTimestamps := make([]int64, len(blocks))
	statuses := make([]string, len(blocks))

	for i, block := range blocks {
		chainIDs[i] = string(block.ChainID)
		blockNumbers[i] = int64(block.Number)
		blockHashes[i] = block.Hash
		parentHashes[i] = block.ParentHash
		blockTimestamps[i] = int64(block.Timestamp)
		statuses[i] = string(block.Status)
	}

	// Record batch size metric
	metrics.DBBatchSize.WithLabelValues("save_blocks").Observe(float64(len(blocks)))

	return u.queries.CreateBlocksBatch(ctx, sqlc.CreateBlocksBatchParams{
		Column1: chainIDs,
		Column2: blockNumbers,
		Column3: blockHashes,
		Column4: parentHashes,
		Column5: blockTimestamps,
		Column6: statuses,
	})
}

// SaveTransactions saves multiple transactions in the transaction using multi-row INSERT.
func (u *UnitOfWork) SaveTransactions(ctx context.Context, txs []*domain.Transaction) error {
	if len(txs) == 0 {
		return nil
	}

	now := time.Now().Unix()

	chainIDs := make([]string, len(txs))
	txHashes := make([]string, len(txs))
	blockNumbers := make([]int64, len(txs))
	blockHashes := make([]string, len(txs))
	txIndexes := make([]int32, len(txs))
	fromAddresses := make([]string, len(txs))
	toAddresses := make([]string, len(txs))
	values := make([]string, len(txs))
	txTypes := make([]string, len(txs))
	tokenAddresses := make([]string, len(txs))
	tokenAmounts := make([]string, len(txs))
	gasUseds := make([]int64, len(txs))
	gasPrices := make([]string, len(txs))
	statuses := make([]string, len(txs))
	blockTimestamps := make([]int64, len(txs))
	rawDatas := make([]string, len(txs))
	createdAts := make([]int64, len(txs))

	for i, t := range txs {
		blockHash := t.BlockHash
		if blockHash == "" {
			blockHash = "0x"
		}

		chainID := string(t.ChainID)
		if chainID == "" {
			chainID = "unknown"
		}

		chainIDs[i] = chainID
		txHashes[i] = t.Hash
		blockNumbers[i] = int64(t.BlockNumber)
		blockHashes[i] = blockHash
		txIndexes[i] = int32(t.Index)
		fromAddresses[i] = t.From
		toAddresses[i] = t.To
		values[i] = t.Value
		txTypes[i] = string(t.Type)
		tokenAddresses[i] = t.TokenAddress
		tokenAmounts[i] = t.TokenAmount
		gasUseds[i] = int64(t.GasUsed)
		gasPrices[i] = t.GasPrice
		statuses[i] = string(t.Status)
		blockTimestamps[i] = int64(t.Timestamp)
		if len(t.RawData) > 0 {
			rawDatas[i] = string(t.RawData)
		} else {
			rawDatas[i] = "null"
		}
		createdAts[i] = now
	}

	// Record batch size metric
	metrics.DBBatchSize.WithLabelValues("save_transactions").Observe(float64(len(txs)))

	return u.queries.CreateTransactionsBatch(ctx, sqlc.CreateTransactionsBatchParams{
		Column1:  chainIDs,
		Column2:  txHashes,
		Column3:  blockNumbers,
		Column4:  blockHashes,
		Column5:  txIndexes,
		Column6:  fromAddresses,
		Column7:  toAddresses,
		Column8:  values,
		Column9:  txTypes,
		Column10: tokenAddresses,
		Column11: tokenAmounts,
		Column12: gasUseds,
		Column13: gasPrices,
		Column14: statuses,
		Column15: blockTimestamps,
		Column16: rawDatas,
		Column17: createdAts,
	})
}

// AdvanceCursor updates the cursor to a new block within the transaction.
func (u *UnitOfWork) AdvanceCursor(ctx context.Context, chainID domain.ChainID, blockNumber uint64, blockHash string) error {
	return u.queries.UpsertCursorBlock(ctx, sqlc.UpsertCursorBlockParams{
		ChainID:     string(chainID),
		BlockNumber: int64(blockNumber),
		BlockHash:   blockHash,
		UpdatedAt:   sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
	})
}
