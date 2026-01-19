-- name: CreateTransaction :exec
INSERT INTO transactions (
    chain_id,
    tx_hash,
    block_number,
    from_address,
    to_address,
    value,
    gas_used,
    gas_price,
    status,
    block_timestamp,
    created_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
) ON CONFLICT (chain_id, tx_hash, block_number) DO NOTHING;

-- name: GetTransactionByHash :one
SELECT * FROM transactions
WHERE chain_id = $1 AND tx_hash = $2
LIMIT 1;

-- name: GetTransactionsByBlock :many
SELECT * FROM transactions
WHERE chain_id = $1 AND block_number = $2;

-- name: UpdateTransactionStatus :exec
UPDATE transactions
SET status = $1
WHERE chain_id = $2 AND tx_hash = $3;

-- name: DeleteTransactionsByBlock :exec
DELETE FROM transactions
WHERE chain_id = $1 AND block_number = $2;

-- name: DeleteTransactionsOlderThan :exec
DELETE FROM transactions
WHERE chain_id = $1 AND block_timestamp < $2;
