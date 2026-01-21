-- name: CreateWalletAddress :exec
INSERT INTO wallet_addresses (address, network_type, standard)
VALUES ($1, $2, $3)
ON CONFLICT (address, network_type) DO UPDATE
SET
    standard = EXCLUDED.standard;

-- name: GetWalletAddress :one
SELECT id, address, network_type, standard, created_at, updated_at
FROM wallet_addresses
WHERE address = $1;

-- name: ListWalletAddresses :many
SELECT id, address, network_type, standard, created_at, updated_at
FROM wallet_addresses;
