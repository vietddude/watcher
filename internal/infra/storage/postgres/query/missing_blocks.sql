-- name: CreateMissingBlock :exec
INSERT INTO missing_blocks (chain_id, from_block, to_block, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, NOW(), NOW());

-- name: GetNextMissingBlock :one
SELECT id, chain_id, from_block, to_block, status, created_at, updated_at
FROM missing_blocks
WHERE chain_id = $1 AND status = 'pending'
ORDER BY from_block ASC
LIMIT 1;

-- name: MarkMissingBlockProcessing :exec
UPDATE missing_blocks SET status = 'processing', updated_at = NOW() WHERE id = $1;

-- name: MarkMissingBlockCompleted :exec
UPDATE missing_blocks SET status = 'completed', updated_at = NOW() WHERE id = $1;

-- name: MarkMissingBlockFailed :exec
UPDATE missing_blocks SET status = 'failed', error_msg = $2, updated_at = NOW() WHERE id = $1;

-- name: GetPendingMissingBlocks :many
SELECT id, chain_id, from_block, to_block, status
FROM missing_blocks
WHERE chain_id = $1 AND status = 'pending'
ORDER BY from_block ASC
LIMIT 1000;

-- name: CountPendingMissingBlocks :one
SELECT COUNT(*) 
FROM missing_blocks 
WHERE chain_id = $1 AND status = 'pending';
