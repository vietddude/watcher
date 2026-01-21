package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/storage/postgres/sqlc"
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
	err := r.db.Queries.CreateWalletAddress(ctx, sqlc.CreateWalletAddressParams{
		Address:     wallet.Address,
		NetworkType: string(wallet.Type),
		Standard:    string(wallet.Standard),
	})
	if err != nil {
		return fmt.Errorf("failed to save wallet address: %w", err)
	}
	return nil
}

// GetByAddress retrieves a wallet by address.
func (r *WalletRepo) GetByAddress(
	ctx context.Context,
	address string,
) (*domain.WalletAddress, error) {
	row, err := r.db.Queries.GetWalletAddress(ctx, address)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get wallet address: %w", err)
	}

	return &domain.WalletAddress{
		Address:  row.Address,
		Type:     domain.NetworkType(row.NetworkType),
		Standard: domain.AddressStandard(row.Standard),
	}, nil
}

// GetAll retrieves all wallet addresses.
func (r *WalletRepo) GetAll(ctx context.Context) ([]*domain.WalletAddress, error) {
	rows, err := r.db.Queries.ListWalletAddresses(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get all wallet addresses: %w", err)
	}

	var wallets []*domain.WalletAddress
	for _, row := range rows {
		wallets = append(wallets, &domain.WalletAddress{
			Address:  row.Address,
			Type:     domain.NetworkType(row.NetworkType),
			Standard: domain.AddressStandard(row.Standard),
		})
	}
	return wallets, nil
}
