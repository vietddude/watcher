package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/vietddude/watcher/internal/core/domain"
)

type TxRepo struct {
	db *sql.DB
}

func NewTxRepo(db *PostgresDB) *TxRepo {
	return &TxRepo{db: db.DB}
}

func (r *TxRepo) Save(ctx context.Context, tx *domain.Transaction) error {
	query := `
		INSERT INTO transactions (
			chain_id, tx_hash, block_number, block_hash, tx_index, 
			from_addr, to_addr, value, gas_used, gas_price, 
			status, timestamp, raw_data, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW())
		ON CONFLICT (chain_id, tx_hash) DO NOTHING;
	`
	// Note: We use DO NOTHING for txs as they are immutable usually, or update if status changes?
	// If status can change (reorg), we might need update. But reorg handles deletes or updates.
	// Let's stick to DO NOTHING. UpdateStatus handles status changes.

	_, err := r.db.ExecContext(ctx, query,
		tx.ChainID, tx.TxHash, tx.BlockNumber, tx.BlockHash, tx.TxIndex,
		tx.From, tx.To, tx.Value, tx.GasUsed, tx.GasPrice,
		tx.Status, tx.Timestamp, tx.RawData,
	)
	if err != nil {
		return fmt.Errorf("save tx: %w", err)
	}
	return nil
}

func (r *TxRepo) SaveBatch(ctx context.Context, txs []*domain.Transaction) error {
	if len(txs) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
		INSERT INTO transactions (
			chain_id, tx_hash, block_number, block_hash, tx_index, 
			from_addr, to_addr, value, gas_used, gas_price, 
			status, timestamp, raw_data, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW())
		ON CONFLICT (chain_id, tx_hash) DO NOTHING;
	`
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, t := range txs {
		_, err := stmt.ExecContext(ctx,
			t.ChainID, t.TxHash, t.BlockNumber, t.BlockHash, t.TxIndex,
			t.From, t.To, t.Value, t.GasUsed, t.GasPrice,
			t.Status, t.Timestamp, t.RawData,
		)
		if err != nil {
			return fmt.Errorf("save batch tx: %w", err)
		}
	}

	return tx.Commit()
}

func (r *TxRepo) GetByHash(ctx context.Context, chainID string, hash string) (*domain.Transaction, error) {
	query := `
		SELECT 
			chain_id, tx_hash, block_number, block_hash, tx_index, 
			from_addr, to_addr, value, gas_used, gas_price, 
			status, timestamp, raw_data 
		FROM transactions 
		WHERE chain_id = $1 AND tx_hash = $2
	`
	row := r.db.QueryRowContext(ctx, query, chainID, hash)
	return scanTx(row)
}

func (r *TxRepo) GetByBlock(ctx context.Context, chainID string, number uint64) ([]*domain.Transaction, error) {
	query := `
		SELECT 
			chain_id, tx_hash, block_number, block_hash, tx_index, 
			from_addr, to_addr, value, gas_used, gas_price, 
			status, timestamp, raw_data 
		FROM transactions 
		WHERE chain_id = $1 AND block_number = $2
	`
	rows, err := r.db.QueryContext(ctx, query, chainID, number)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txs []*domain.Transaction
	for rows.Next() {
		tx, err := scanTx(rows)
		if err != nil {
			return nil, err
		}
		txs = append(txs, tx)
	}
	return txs, nil
}

func (r *TxRepo) UpdateStatus(ctx context.Context, chainID string, hash string, status domain.TxStatus) error {
	query := `UPDATE transactions SET status = $1 WHERE chain_id = $2 AND tx_hash = $3`
	_, err := r.db.ExecContext(ctx, query, status, chainID, hash)
	return err
}

func (r *TxRepo) DeleteByBlock(ctx context.Context, chainID string, number uint64) error {
	query := `DELETE FROM transactions WHERE chain_id = $1 AND block_number = $2`
	_, err := r.db.ExecContext(ctx, query, chainID, number)
	return err
}

func scanTx(scanner interface{ Scan(...interface{}) error }) (*domain.Transaction, error) {
	var tx domain.Transaction
	var status string
	var rawData []byte

	err := scanner.Scan(
		&tx.ChainID, &tx.TxHash, &tx.BlockNumber, &tx.BlockHash, &tx.TxIndex,
		&tx.From, &tx.To, &tx.Value, &tx.GasUsed, &tx.GasPrice,
		&status, &tx.Timestamp, &rawData,
	)
	if err == sql.ErrNoRows {
		return nil, nil // Not found
	}
	if err != nil {
		return nil, err
	}

	tx.Status = domain.TxStatus(status)
	tx.RawData = rawData
	return &tx, nil
}
