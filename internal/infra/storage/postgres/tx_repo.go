package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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
	err := r.db.Queries.CreateTransaction(ctx, sqlc.CreateTransactionParams{
		ChainID:        string(tx.ChainID),
		TxHash:         tx.Hash,
		BlockNumber:    int64(tx.BlockNumber),
		FromAddress:    tx.From,
		ToAddress:      sql.NullString{String: tx.To, Valid: tx.To != ""},
		Value:          sql.NullString{String: tx.Value, Valid: tx.Value != ""},
		GasUsed:        sql.NullInt64{Int64: int64(tx.GasUsed), Valid: true},
		GasPrice:       sql.NullString{String: tx.GasPrice, Valid: tx.GasPrice != ""},
		Status:         sql.NullString{String: string(tx.Status), Valid: string(tx.Status) != ""},
		CreatedAt:      sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
		BlockTimestamp: int64(tx.Timestamp),
	})
	if err != nil {
		return fmt.Errorf("failed to save transaction: %w", err)
	}
	return nil
}

// SaveBatch saves multiple transactions.
func (r *TxRepo) SaveBatch(ctx context.Context, txs []*domain.Transaction) error {
	if len(txs) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)

	if err != nil {
		return err
	}
	defer tx.Rollback()

	qtx := r.db.Queries.WithTx(tx)

	for _, t := range txs {
		err := qtx.CreateTransaction(ctx, sqlc.CreateTransactionParams{
			ChainID:        string(t.ChainID),
			TxHash:         t.Hash,
			BlockNumber:    int64(t.BlockNumber),
			FromAddress:    t.From,
			ToAddress:      sql.NullString{String: t.To, Valid: t.To != ""},
			Value:          sql.NullString{String: t.Value, Valid: t.Value != ""},
			GasUsed:        sql.NullInt64{Int64: int64(t.GasUsed), Valid: true},
			GasPrice:       sql.NullString{String: t.GasPrice, Valid: t.GasPrice != ""},
			Status:         sql.NullString{String: string(t.Status), Valid: string(t.Status) != ""},
			CreatedAt:      sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
			BlockTimestamp: int64(t.Timestamp),
		})
		if err != nil {
			return err
		}
	}

	return tx.Commit()
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
		ChainID:     domain.ChainID(row.ChainID),
		Hash:        row.TxHash,
		BlockNumber: uint64(row.BlockNumber),
		From:        row.FromAddress,
		To:          row.ToAddress.String,
		Value:       row.Value.String,
		GasUsed:     uint64(row.GasUsed.Int64),
		GasPrice:    row.GasPrice.String,
		Status:      domain.TxStatus(row.Status.String),
		Timestamp:   uint64(row.BlockTimestamp), // Use BlockTimestamp as it is NOT NULL in schema
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
