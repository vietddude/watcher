-- name: UpsertCursor :exec
INSERT INTO cursors (chain_id, block_number, block_hash, state, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (chain_id) DO UPDATE SET
    block_number = EXCLUDED.block_number,
    block_hash = EXCLUDED.block_hash,
    state = EXCLUDED.state,
    updated_at = $5;

-- name: GetCursor :one
SELECT chain_id, block_number, block_hash, state, updated_at
FROM cursors
WHERE chain_id = $1;

-- name: UpdateCursorBlock :exec
UPDATE cursors 
SET block_number = $1, block_hash = $2, updated_at = $4
WHERE chain_id = $3;

-- name: UpdateCursorState :exec
UPDATE cursors 
SET state = $1, updated_at = $3
WHERE chain_id = $2;

-- name: UpsertCursorBlock :exec
INSERT INTO cursors (chain_id, block_number, block_hash, state, updated_at)
VALUES ($1, $2, $3, 'running', $4)
ON CONFLICT (chain_id) DO UPDATE SET
    block_number = EXCLUDED.block_number,
    block_hash = EXCLUDED.block_hash,
    updated_at = $4;
