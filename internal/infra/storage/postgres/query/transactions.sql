-- name: CreateTransaction :exec
INSERT INTO transactions (
    chain_id,
    tx_hash,
    block_number,
    block_hash,
    tx_index,
    from_address,
    to_address,
    value,
    tx_type,
    token_address,
    token_amount,
    gas_used,
    gas_price,
    status,
    block_timestamp,
    raw_data,
    created_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17
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

-- name: CreateTransactionsBatch :exec
INSERT INTO transactions (
    chain_id, tx_hash, block_number, block_hash, tx_index,
    from_address, to_address, value, tx_type, token_address,
    token_amount, gas_used, gas_price, status, block_timestamp, raw_data, created_at
)
SELECT
    unnest($1::varchar[]) AS chain_id,
    unnest($2::varchar[]) AS tx_hash,
    unnest($3::bigint[]) AS block_number,
    unnest($4::varchar[]) AS block_hash,
    unnest($5::int[]) AS tx_index,
    unnest($6::varchar[]) AS from_address,
    unnest($7::varchar[]) AS to_address,
    unnest($8::varchar[]) AS value,
    unnest($9::varchar[]) AS tx_type,
    unnest($10::varchar[]) AS token_address,
    unnest($11::varchar[]) AS token_amount,
    unnest($12::bigint[]) AS gas_used,
    unnest($13::varchar[]) AS gas_price,
    unnest($14::varchar[]) AS status,
    unnest($15::bigint[]) AS block_timestamp,
    unnest($16::text[])::jsonb AS raw_data,
    unnest($17::bigint[]) AS created_at
ON CONFLICT (chain_id, tx_hash, block_number) DO NOTHING;

