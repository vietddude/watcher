-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- Safe reset for init schema (dev env)
DROP TABLE IF EXISTS failed_blocks CASCADE;
DROP TABLE IF EXISTS missing_blocks CASCADE;
DROP TABLE IF EXISTS transactions CASCADE;
DROP TABLE IF EXISTS blocks CASCADE;
DROP TABLE IF EXISTS cursors CASCADE;
DROP TABLE IF EXISTS wallet_addresses CASCADE;

--------------------------------------------------
-- Cursors
--------------------------------------------------
CREATE TABLE cursors (
    chain_id VARCHAR(64) PRIMARY KEY,
    block_number BIGINT NOT NULL DEFAULT 0,
    block_hash VARCHAR(128) NOT NULL DEFAULT '',
    state VARCHAR(32) NOT NULL DEFAULT 'scanning', -- scanning, backfill, reorg, paused
    created_at BIGINT DEFAULT (extract(epoch from now())::bigint),
    updated_at BIGINT DEFAULT (extract(epoch from now())::bigint)
);

--------------------------------------------------
-- Blocks
--------------------------------------------------
CREATE TABLE blocks (
    chain_id VARCHAR(64) NOT NULL,
    block_number BIGINT NOT NULL,
    block_hash VARCHAR(128) NOT NULL,
    parent_hash VARCHAR(128) NOT NULL,
    block_timestamp BIGINT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'finalized', -- pending, finalized, orphaned
    created_at BIGINT DEFAULT (extract(epoch from now())::bigint),

    PRIMARY KEY (chain_id, block_number)
);

CREATE UNIQUE INDEX uq_blocks_chain_hash
ON blocks(chain_id, block_hash);

CREATE INDEX idx_blocks_timestamp
ON blocks(chain_id, block_timestamp);

CREATE INDEX idx_blocks_chain_parent
ON blocks(chain_id, parent_hash);

--------------------------------------------------
-- Transactions
--------------------------------------------------
CREATE TABLE transactions (
    chain_id VARCHAR(64) NOT NULL,
    tx_hash VARCHAR(128) NOT NULL,
    block_number BIGINT NOT NULL,
    block_hash VARCHAR(128) NOT NULL,
    tx_index INT NOT NULL,
    from_address VARCHAR(128) NOT NULL,
    to_address VARCHAR(128) DEFAULT '',
    value VARCHAR(128) DEFAULT '0',
    gas_used BIGINT DEFAULT 0,
    gas_price VARCHAR(128) DEFAULT '0',
    status VARCHAR(32) DEFAULT 'success', -- success, failed, orphaned
    block_timestamp BIGINT NOT NULL,
    raw_data JSONB,
    created_at BIGINT DEFAULT (extract(epoch from now())::bigint),

    PRIMARY KEY (chain_id, tx_hash, block_number)
);

CREATE INDEX idx_transactions_chain_block
ON transactions(chain_id, block_number);

CREATE INDEX idx_transactions_chain_from
ON transactions(chain_id, from_address);

CREATE INDEX idx_transactions_chain_to
ON transactions(chain_id, to_address);

CREATE INDEX idx_transactions_chain_status_time
ON transactions(chain_id, status, block_timestamp);

--------------------------------------------------
-- Missing Blocks Queue
--------------------------------------------------
CREATE TABLE missing_blocks (
    id SERIAL PRIMARY KEY,
    chain_id VARCHAR(64) NOT NULL,
    from_block BIGINT NOT NULL,
    to_block BIGINT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    attempts INT NOT NULL DEFAULT 0,
    error_msg TEXT,
    created_at BIGINT DEFAULT (extract(epoch from now())::bigint),
    updated_at BIGINT DEFAULT (extract(epoch from now())::bigint)
);

CREATE INDEX idx_missing_blocks_chain_status
ON missing_blocks(chain_id, status);

--------------------------------------------------
-- Failed Blocks Queue
--------------------------------------------------
CREATE TABLE failed_blocks (
    id SERIAL PRIMARY KEY,
    chain_id VARCHAR(64) NOT NULL,
    block_number BIGINT NOT NULL,
    error_msg TEXT,
    retry_count INT NOT NULL DEFAULT 0,
    last_attempt BIGINT DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'pending', -- pending, resolved, abandoned
    created_at BIGINT DEFAULT (extract(epoch from now())::bigint),
    updated_at BIGINT DEFAULT (extract(epoch from now())::bigint)
);

CREATE INDEX idx_failed_blocks_chain_status
ON failed_blocks(chain_id, status);

--------------------------------------------------
-- Wallet Addresses (Bloom input only)
--------------------------------------------------
CREATE TABLE wallet_addresses (
    id SERIAL PRIMARY KEY,
    address VARCHAR(128) NOT NULL,
    network_type VARCHAR(64) NOT NULL,
    standard VARCHAR(64) NOT NULL,
    created_at BIGINT DEFAULT (extract(epoch from now())::bigint),
    updated_at BIGINT DEFAULT (extract(epoch from now())::bigint)
);

CREATE UNIQUE INDEX uq_wallet_addresses_unique
ON wallet_addresses(address, network_type);

CREATE INDEX idx_wallet_addresses_network
ON wallet_addresses(network_type);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';
DROP TABLE IF EXISTS failed_blocks;
DROP TABLE IF EXISTS missing_blocks;
DROP TABLE IF EXISTS transactions;
DROP TABLE IF EXISTS blocks;
DROP TABLE IF EXISTS cursors;
DROP TABLE IF EXISTS wallet_addresses;
-- +goose StatementEnd
