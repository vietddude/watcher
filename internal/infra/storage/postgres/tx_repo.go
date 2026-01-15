package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
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
	query := `
		INSERT INTO transactions (
			chain_id, tx_hash, block_number, from_address, to_address, value, gas_used, gas_price, status, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		ON CONFLICT (chain_id, tx_hash) DO UPDATE SET
			block_number = EXCLUDED.block_number,
			status = EXCLUDED.status
	`
	// Note: Schema in DB might differ slightly from domain struct which has more fields (e.g. BlockHash, TxIndex, RawData etc. if added later).
	// Based on migration:
	// chain_id, tx_hash, block_number, from_address, to_address, value, gas_used, gas_price, status, created_at

	_, err := r.db.ExecContext(ctx, query,
		tx.ChainID, tx.TxHash, tx.BlockNumber,
		tx.From, tx.To, tx.Value, tx.GasUsed, tx.GasPrice,
		string(tx.Status),
	)
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

	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
		INSERT INTO transactions (
			chain_id, tx_hash, block_number, from_address, to_address, value, gas_used, gas_price, status, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		ON CONFLICT (chain_id, tx_hash) DO UPDATE SET
			block_number = EXCLUDED.block_number,
			status = EXCLUDED.status
	`

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, t := range txs {
		_, err := stmt.ExecContext(ctx,
			t.ChainID, t.TxHash, t.BlockNumber,
			t.From, t.To, t.Value, t.GasUsed, t.GasPrice,
			string(t.Status),
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

type txRow struct {
	ChainID     string    `db:"chain_id"`
	TxHash      string    `db:"tx_hash"`
	BlockNumber uint64    `db:"block_number"`
	From        string    `db:"from_address"`
	To          *string   `db:"to_address"` // Nullable
	Value       string    `db:"value"`
	GasUsed     uint64    `db:"gas_used"`
	GasPrice    string    `db:"gas_price"`
	Status      string    `db:"status"`
	CreatedAt   time.Time `db:"created_at"`
}

func (t *txRow) toDomain() *domain.Transaction {
	tx := &domain.Transaction{
		ChainID:     t.ChainID,
		TxHash:      t.TxHash,
		BlockNumber: t.BlockNumber,
		From:        t.From,
		Value:       t.Value,
		GasUsed:     t.GasUsed,
		GasPrice:    t.GasPrice,
		Status:      domain.TxStatus(t.Status),
	}
	if t.To != nil {
		tx.To = *t.To
	}
	return tx
}

// GetByHash retrieves a transaction by hash.
func (r *TxRepo) GetByHash(ctx context.Context, chainID string, txHash string) (*domain.Transaction, error) {
	query := `
		SELECT chain_id, tx_hash, block_number, from_address, to_address, value, gas_used, gas_price, status, created_at
		FROM transactions
		WHERE chain_id = $1 AND tx_hash = $2
	`

	var row txRow
	err := r.db.GetContext(ctx, &row, query, chainID, txHash)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction: %w", err)
	}

	return row.toDomain(), nil
}

// GetByBlock retrieves all transactions in a block.
func (r *TxRepo) GetByBlock(ctx context.Context, chainID string, blockNumber uint64) ([]*domain.Transaction, error) {
	query := `
		SELECT chain_id, tx_hash, block_number, from_address, to_address, value, gas_used, gas_price, status, created_at
		FROM transactions
		WHERE chain_id = $1 AND block_number = $2
	`

	var rows []txRow
	err := r.db.SelectContext(ctx, &rows, query, chainID, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get transactions: %w", err)
	}

	var txs []*domain.Transaction
	for _, row := range rows {
		txs = append(txs, row.toDomain())
	}
	return txs, nil
}

// UpdateStatus updates transaction status.
func (r *TxRepo) UpdateStatus(ctx context.Context, chainID string, txHash string, status domain.TxStatus) error {
	query := `UPDATE transactions SET status = $1 WHERE chain_id = $2 AND tx_hash = $3`
	_, err := r.db.ExecContext(ctx, query, string(status), chainID, txHash)
	return err
}

// DeleteByBlock deletes transactions in a block.
func (r *TxRepo) DeleteByBlock(ctx context.Context, chainID string, blockNumber uint64) error {
	query := `DELETE FROM transactions WHERE chain_id = $1 AND block_number = $2`
	_, err := r.db.ExecContext(ctx, query, chainID, blockNumber)
	return err
}
