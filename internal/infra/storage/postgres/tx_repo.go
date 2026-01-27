package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/sqlc-dev/pqtype"
	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/storage/postgres/sqlc"
)

// TxRepo implements storage.TransactionRepository using PostgreSQL.
type TxRepo struct {
	db *DB
}

// NewTxRepo creates a new PostgreSQL transaction repository.
func NewTxRepo(db *DB) *TxRepo {
	return &TxRepo{db: db}
}

// Save saves a transaction to the database.
func (r *TxRepo) Save(ctx context.Context, tx *domain.Transaction) error {
	if tx.ChainID == "" {
		return fmt.Errorf("transaction missing chain ID")
	}

	blockHash := tx.BlockHash
	if blockHash == "" {
		blockHash = "0x" // Safe default to avoid NULL constraint violation
	}

	err := r.db.Queries.CreateTransaction(ctx, sqlc.CreateTransactionParams{
		ChainID:        string(tx.ChainID),
		TxHash:         tx.Hash,
		BlockNumber:    int64(tx.BlockNumber),
		BlockHash:      blockHash,
		TxIndex:        int32(tx.Index),
		FromAddress:    tx.From,
		ToAddress:      sql.NullString{String: tx.To, Valid: tx.To != ""},
		Value:          sql.NullString{String: tx.Value, Valid: tx.Value != ""},
		TxType:         sql.NullString{String: string(tx.Type), Valid: tx.Type != ""},
		TokenAddress:   sql.NullString{String: tx.TokenAddress, Valid: tx.TokenAddress != ""},
		TokenAmount:    sql.NullString{String: tx.TokenAmount, Valid: tx.TokenAmount != ""},
		GasUsed:        sql.NullInt64{Int64: int64(tx.GasUsed), Valid: true},
		GasPrice:       sql.NullString{String: tx.GasPrice, Valid: tx.GasPrice != ""},
		Status:         sql.NullString{String: string(tx.Status), Valid: string(tx.Status) != ""},
		CreatedAt:      sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
		BlockTimestamp: int64(tx.Timestamp),
		RawData:        pqtype.NullRawMessage{RawMessage: tx.RawData, Valid: len(tx.RawData) > 0},
	})
	if err != nil {
		return fmt.Errorf("failed to save transaction: %w", err)
	}
	return nil
}

// SaveBatch saves multiple transactions using a single multi-row INSERT.
func (r *TxRepo) SaveBatch(ctx context.Context, txs []*domain.Transaction) error {
	if len(txs) == 0 {
		return nil
	}

	now := time.Now().Unix()

	// Prepare arrays for UNNEST
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
			// Fallback to a safe value or log error.
			// In production, ChainID should NEVER be empty here.
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

	return r.db.Queries.CreateTransactionsBatch(ctx, sqlc.CreateTransactionsBatchParams{
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

// GetByHash retrieves a transaction by hash.
func (r *TxRepo) GetByHash(
	ctx context.Context,
	chainID domain.ChainID,
	txHash string,
) (*domain.Transaction, error) {
	row, err := r.db.Queries.GetTransactionByHash(ctx, sqlc.GetTransactionByHashParams{
		ChainID: string(chainID),
		TxHash:  txHash,
	})
	if err == sql.ErrNoRows {
		return nil, nil // Not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction: %w", err)
	}

	return r.toDomain(row), nil
}

// GetByBlock retrieves all transactions in a block.
func (r *TxRepo) GetByBlock(
	ctx context.Context,
	chainID domain.ChainID,
	blockNumber uint64,
) ([]*domain.Transaction, error) {
	rows, err := r.db.Queries.GetTransactionsByBlock(ctx, sqlc.GetTransactionsByBlockParams{
		ChainID:     string(chainID),
		BlockNumber: int64(blockNumber),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get transactions: %w", err)
	}

	var txs []*domain.Transaction
	for _, row := range rows {
		txs = append(txs, r.toDomain(row))
	}
	return txs, nil
}

// UpdateStatus updates transaction status.
func (r *TxRepo) UpdateStatus(
	ctx context.Context,
	chainID domain.ChainID,
	txHash string,
	status domain.TxStatus,
) error {
	err := r.db.Queries.UpdateTransactionStatus(ctx, sqlc.UpdateTransactionStatusParams{
		Status:  sql.NullString{String: string(status), Valid: string(status) != ""},
		ChainID: string(chainID),
		TxHash:  txHash,
	})
	return err
}

// DeleteByBlock deletes transactions in a block.
func (r *TxRepo) DeleteByBlock(
	ctx context.Context,
	chainID domain.ChainID,
	blockNumber uint64,
) error {
	err := r.db.Queries.DeleteTransactionsByBlock(ctx, sqlc.DeleteTransactionsByBlockParams{
		ChainID:     string(chainID),
		BlockNumber: int64(blockNumber),
	})
	return err
}

func (r *TxRepo) toDomain(row sqlc.Transaction) *domain.Transaction {
	tx := &domain.Transaction{
		ChainID:      domain.ChainID(row.ChainID),
		Hash:         row.TxHash,
		BlockNumber:  uint64(row.BlockNumber),
		BlockHash:    row.BlockHash,
		Index:        int(row.TxIndex),
		From:         row.FromAddress,
		To:           row.ToAddress.String,
		Value:        row.Value.String,
		Type:         domain.TxType(row.TxType.String),
		TokenAddress: row.TokenAddress.String,
		TokenAmount:  row.TokenAmount.String,
		GasUsed:      uint64(row.GasUsed.Int64),
		GasPrice:     row.GasPrice.String,
		Status:       domain.TxStatus(row.Status.String),
		Timestamp:    uint64(row.BlockTimestamp), // Use BlockTimestamp as it is NOT NULL in schema
		RawData:      row.RawData.RawMessage,
		// Note: CreatedAt is also available in row.CreatedAt
	}
	return tx
}

// DeleteTransactionsOlderThan deletes transactions older than the given timestamp.
func (r *TxRepo) DeleteTransactionsOlderThan(
	ctx context.Context,
	chainID domain.ChainID,
	timestamp uint64,
) error {
	err := r.db.Queries.DeleteTransactionsOlderThan(ctx, sqlc.DeleteTransactionsOlderThanParams{
		ChainID:        string(chainID),
		BlockTimestamp: int64(timestamp),
	})
	if err != nil {
		return fmt.Errorf("failed to delete old transactions: %w", err)
	}
	return nil
}
