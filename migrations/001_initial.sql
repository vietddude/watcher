-- +goose Up
-- Initial schema for watcher blockchain indexer

CREATE TABLE IF NOT EXISTS blocks (
    id SERIAL PRIMARY KEY,
    chain_id VARCHAR(64) NOT NULL,
    block_number BIGINT NOT NULL,
    block_hash VARCHAR(66) NOT NULL,
    parent_hash VARCHAR(66) NOT NULL,
    block_timestamp TIMESTAMPTZ NOT NULL,
    status VARCHAR(20) DEFAULT 'confirmed',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    
    CONSTRAINT uq_blocks_chain_number UNIQUE(chain_id, block_number)
);

CREATE INDEX IF NOT EXISTS idx_blocks_hash ON blocks(chain_id, block_hash);
CREATE INDEX IF NOT EXISTS idx_blocks_status ON blocks(chain_id, status);

CREATE TABLE IF NOT EXISTS transactions (
    id SERIAL PRIMARY KEY,
    chain_id VARCHAR(64) NOT NULL,
    tx_hash VARCHAR(66) NOT NULL,
    block_number BIGINT NOT NULL,
    from_address VARCHAR(42) NOT NULL,
    to_address VARCHAR(42),
    value VARCHAR(78),
    gas_used BIGINT,
    gas_price VARCHAR(78),
    status VARCHAR(20) DEFAULT 'success',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    
    CONSTRAINT uq_tx_chain_hash UNIQUE(chain_id, tx_hash)
);

CREATE INDEX IF NOT EXISTS idx_tx_block ON transactions(chain_id, block_number);
CREATE INDEX IF NOT EXISTS idx_tx_from ON transactions(chain_id, from_address);
CREATE INDEX IF NOT EXISTS idx_tx_to ON transactions(chain_id, to_address);

CREATE TABLE IF NOT EXISTS cursors (
    chain_id VARCHAR(64) PRIMARY KEY,
    block_number BIGINT NOT NULL,
    block_hash VARCHAR(66) NOT NULL,
    state VARCHAR(20) DEFAULT 'running',
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS missing_blocks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chain_id VARCHAR(64) NOT NULL,
    from_block BIGINT NOT NULL,
    to_block BIGINT NOT NULL,
    status VARCHAR(20) DEFAULT 'pending',
    error_msg TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_missing_chain_status ON missing_blocks(chain_id, status);

CREATE TABLE IF NOT EXISTS failed_blocks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chain_id VARCHAR(64) NOT NULL,
    block_number BIGINT NOT NULL,
    failure_type VARCHAR(50),
    error_msg TEXT,
    retry_count INT DEFAULT 0,
    status VARCHAR(20) DEFAULT 'pending',
    last_attempt TIMESTAMPTZ DEFAULT NOW(),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_failed_chain_status ON failed_blocks(chain_id, status);

CREATE TABLE IF NOT EXISTS wallet_addresses (
    id SERIAL PRIMARY KEY,
    address VARCHAR(255) NOT NULL,
    type VARCHAR(20) NOT NULL,
    standard VARCHAR(20) DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),

    CONSTRAINT uq_wallet_address UNIQUE(address)
);

CREATE INDEX IF NOT EXISTS idx_wallet_address ON wallet_addresses(address);

-- +goose Down
DROP TABLE IF EXISTS failed_blocks;
DROP TABLE IF EXISTS missing_blocks;
DROP TABLE IF EXISTS cursors;
DROP TABLE IF EXISTS transactions;
DROP TABLE IF EXISTS blocks;
DROP TABLE IF EXISTS wallet_addresses;
