-- name: CreateBlock :exec
INSERT INTO blocks (
    chain_id,
    block_number,
    block_hash,
    parent_hash,
    block_timestamp,
    status
) VALUES (
    $1, $2, $3, $4, $5, $6
) ON CONFLICT (chain_id, block_number) DO UPDATE
SET
    block_hash = EXCLUDED.block_hash,
    parent_hash = EXCLUDED.parent_hash,
    block_timestamp = EXCLUDED.block_timestamp,
    status = EXCLUDED.status;

-- name: GetBlockByNumber :one
SELECT * FROM blocks
WHERE chain_id = $1 AND block_number = $2
LIMIT 1;

-- name: GetBlockByHash :one
SELECT * FROM blocks
WHERE chain_id = $1 AND block_hash = $2
LIMIT 1;

-- name: GetLatestBlock :one
SELECT * FROM blocks
WHERE chain_id = $1
ORDER BY block_number DESC
LIMIT 1;

-- name: UpdateBlockStatus :exec
UPDATE blocks
SET status = $1
WHERE chain_id = $2 AND block_number = $3;

-- name: FindGaps :many
SELECT
    t1.block_number + 1 AS from_block,
    MIN(t2.block_number) - 1 AS to_block
FROM blocks AS t1
JOIN blocks AS t2 ON t1.chain_id = t2.chain_id AND t1.block_number < t2.block_number
WHERE t1.chain_id = $1
  AND t1.block_number >= $2
  AND t2.block_number <= $3
GROUP BY t1.block_number
HAVING t1.block_number < MIN(t2.block_number) - 1;

-- name: DeleteBlocksInRange :exec
DELETE FROM blocks
WHERE chain_id = $1 AND block_number BETWEEN $2 AND $3;

-- name: DeleteBlocksOlderThan :exec
DELETE FROM blocks
WHERE chain_id = $1 AND block_timestamp < $2;

-- name: CreateBlocksBatch :exec
INSERT INTO blocks (chain_id, block_number, block_hash, parent_hash, block_timestamp, status)
SELECT
    unnest($1::varchar[]) AS chain_id,
    unnest($2::bigint[]) AS block_number,
    unnest($3::varchar[]) AS block_hash,
    unnest($4::varchar[]) AS parent_hash,
    unnest($5::bigint[]) AS block_timestamp,
    unnest($6::varchar[]) AS status
ON CONFLICT (chain_id, block_number) DO UPDATE
SET block_hash = EXCLUDED.block_hash,
    parent_hash = EXCLUDED.parent_hash,
    block_timestamp = EXCLUDED.block_timestamp,
    status = EXCLUDED.status;

