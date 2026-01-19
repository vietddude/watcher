-- name: CreateWalletAddress :exec
INSERT INTO wallet_addresses (address, network_type, standard, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (address) DO UPDATE SET
    network_type = EXCLUDED.network_type,
    standard = EXCLUDED.standard,
    updated_at = $5;

-- name: GetWalletAddress :one
SELECT id, address, network_type, standard, created_at, updated_at
FROM wallet_addresses
WHERE address = $1;

-- name: ListWalletAddresses :many
SELECT id, address, network_type, standard, created_at, updated_at
FROM wallet_addresses;
