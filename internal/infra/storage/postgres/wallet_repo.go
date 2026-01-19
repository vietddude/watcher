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

type walletModel struct {
	ID        uint64    `db:"id"`
	Address   string    `db:"address"`
	Network   string    `db:"network_type"`
	Standard  string    `db:"standard"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

func (m *walletModel) toDomain() *domain.WalletAddress {
	return &domain.WalletAddress{
		ID:        m.ID,
		Address:   m.Address,
		Type:      domain.NetworkType(m.Network),
		Standard:  domain.AddressStandard(m.Standard),
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}

// Save saves a wallet address to the database.
func (r *WalletRepo) Save(ctx context.Context, wallet *domain.WalletAddress) error {
	query := `
		INSERT INTO wallet_addresses (address, network_type, standard, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		ON CONFLICT (address) DO UPDATE SET
			network_type = EXCLUDED.network_type,
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
		SELECT id, address, network_type, standard, created_at, updated_at
		FROM wallet_addresses
		WHERE address = $1
	`

	var model walletModel
	err := r.db.GetContext(ctx, &model, query, address)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get wallet address: %w", err)
	}

	return model.toDomain(), nil
}

// GetAll retrieves all wallet addresses.
func (r *WalletRepo) GetAll(ctx context.Context) ([]*domain.WalletAddress, error) {
	query := `
		SELECT id, address, network_type, standard, created_at, updated_at
		FROM wallet_addresses
	`

	var models []walletModel
	err := r.db.SelectContext(ctx, &models, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get all wallet addresses: %w", err)
	}

	var wallets []*domain.WalletAddress
	for _, m := range models {
		wallets = append(wallets, m.toDomain())
	}
	return wallets, nil
}
