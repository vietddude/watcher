package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
)

// WalletRepo implements storage.WalletRepository using PostgreSQL.
type WalletRepo struct {
	db *DB
}

// NewWalletRepo creates a new PostgreSQL wallet repository.
func NewWalletRepo(db *DB) *WalletRepo {
	return &WalletRepo{db: db}
}

// Save saves a wallet address to the database.
func (r *WalletRepo) Save(ctx context.Context, wallet *domain.WalletAddress) error {
	query := `
		INSERT INTO wallet_addresses (address, type, standard, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		ON CONFLICT (address) DO UPDATE SET
			type = EXCLUDED.type,
			standard = EXCLUDED.standard,
			updated_at = NOW()
	`

	_, err := r.db.ExecContext(ctx, query,
		wallet.Address,
		string(wallet.Type),
		string(wallet.Standard),
	)
	if err != nil {
		return fmt.Errorf("failed to save wallet address: %w", err)
	}
	return nil
}

// GetByAddress retrieves a wallet by address.
func (r *WalletRepo) GetByAddress(ctx context.Context, address string) (*domain.WalletAddress, error) {
	query := `
		SELECT id, address, type, standard, created_at, updated_at
		FROM wallet_addresses
		WHERE address = $1
	`

	var row struct {
		ID        uint64    `db:"id"`
		Address   string    `db:"address"`
		Type      string    `db:"type"`
		Standard  string    `db:"standard"`
		CreatedAt time.Time `db:"created_at"`
		UpdatedAt time.Time `db:"updated_at"`
	}

	err := r.db.GetContext(ctx, &row, query, address)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get wallet address: %w", err)
	}

	return &domain.WalletAddress{
		ID:        row.ID,
		Address:   row.Address,
		Type:      domain.NetworkType(row.Type),
		Standard:  domain.AddressStandard(row.Standard),
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}, nil
}

// GetAll retrieves all wallet addresses.
func (r *WalletRepo) GetAll(ctx context.Context) ([]*domain.WalletAddress, error) {
	query := `
		SELECT id, address, type, standard, created_at, updated_at
		FROM wallet_addresses
	`

	var rows []struct {
		ID        uint64    `db:"id"`
		Address   string    `db:"address"`
		Type      string    `db:"type"`
		Standard  string    `db:"standard"`
		CreatedAt time.Time `db:"created_at"`
		UpdatedAt time.Time `db:"updated_at"`
	}

	err := r.db.SelectContext(ctx, &rows, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get all wallet addresses: %w", err)
	}

	var wallets []*domain.WalletAddress
	for _, row := range rows {
		wallets = append(wallets, &domain.WalletAddress{
			ID:        row.ID,
			Address:   row.Address,
			Type:      domain.NetworkType(row.Type),
			Standard:  domain.AddressStandard(row.Standard),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		})
	}
	return wallets, nil
}
