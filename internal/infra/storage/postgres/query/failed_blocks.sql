-- name: CreateFailedBlock :exec
INSERT INTO failed_blocks (chain_id, block_number, error_msg, retry_count, status, last_attempt, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetNextFailedBlock :one
SELECT id, chain_id, block_number, error_msg, retry_count, last_attempt, status
FROM failed_blocks
WHERE chain_id = $1 AND status = 'pending'
ORDER BY last_attempt ASC
LIMIT 1;

-- name: IncrementFailedBlockRetry :exec
UPDATE failed_blocks 
SET retry_count = retry_count + 1, last_attempt = $2
WHERE id = $1;

-- name: MarkFailedBlockResolved :exec
UPDATE failed_blocks 
SET status = 'resolved' 
WHERE id = $1;

-- name: GetAllFailedBlocks :many
SELECT id, chain_id, block_number, error_msg, retry_count, last_attempt, status
FROM failed_blocks
WHERE chain_id = $1 AND status = 'pending';

-- name: CountFailedBlocks :one
SELECT COUNT(*) 
FROM failed_blocks 
WHERE chain_id = $1 AND status = 'pending';
