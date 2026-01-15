-- Enable UUID extension if needed (though we use string IDs mostly)
-- CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Cursors Table
CREATE TABLE cursors (
    chain_id VARCHAR(64) PRIMARY KEY,
    current_block BIGINT NOT NULL DEFAULT 0,
    current_block_hash VARCHAR(128) NOT NULL DEFAULT '',
    state VARCHAR(32) NOT NULL DEFAULT 'scanning', -- scanning, backfill, reorg, paused
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Blocks Table
CREATE TABLE blocks (
    chain_id VARCHAR(64) NOT NULL,
    number BIGINT NOT NULL,
    hash VARCHAR(128) NOT NULL,
    parent_hash VARCHAR(128) NOT NULL,
    timestamp TIMESTAMP WITH TIME ZONE NOT NULL,
    tx_count INT NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'finalized', -- pending, finalized, orphaned
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    
    PRIMARY KEY (chain_id, number)
);

CREATE INDEX idx_blocks_hash ON blocks(chain_id, hash);
CREATE INDEX idx_blocks_timestamp ON blocks(chain_id, timestamp);

-- Transactions Table
CREATE TABLE transactions (
    chain_id VARCHAR(64) NOT NULL,
    tx_hash VARCHAR(128) NOT NULL,
    block_number BIGINT NOT NULL,
    block_hash VARCHAR(128) NOT NULL,
    tx_index INT NOT NULL,
    from_addr VARCHAR(128) NOT NULL,
    to_addr VARCHAR(128) DEFAULT '',
    value VARCHAR(128) DEFAULT '0', -- Store as string for big integers
    gas_used BIGINT DEFAULT 0,
    gas_price VARCHAR(128) DEFAULT '0',
    status VARCHAR(32) DEFAULT 'success', -- success, failed
    timestamp TIMESTAMP WITH TIME ZONE NOT NULL,
    raw_data JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    PRIMARY KEY (chain_id, tx_hash)
);

CREATE INDEX idx_transactions_block ON transactions(chain_id, block_number);
CREATE INDEX idx_transactions_from ON transactions(chain_id, from_addr);
CREATE INDEX idx_transactions_to ON transactions(chain_id, to_addr);

-- Missing Blocks Queue
CREATE TABLE missing_blocks (
    id SERIAL PRIMARY KEY,
    chain_id VARCHAR(64) NOT NULL,
    from_block BIGINT NOT NULL,
    to_block BIGINT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending', -- pending, processing, completed, failed
    attempts INT NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_missing_blocks_status ON missing_blocks(chain_id, status);

-- Failed Blocks Queue
CREATE TABLE failed_blocks (
    id SERIAL PRIMARY KEY,
    chain_id VARCHAR(64) NOT NULL,
    block_number BIGINT NOT NULL,
    error TEXT,
    retry_count INT NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'pending', -- pending, resolved, abandoned
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_failed_blocks_status ON failed_blocks(chain_id, status);
