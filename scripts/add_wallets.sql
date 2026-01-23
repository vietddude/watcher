-- Helper script to insert wallet addresses into Watcher
-- You can run this directly in your psql terminal or via GUI

-- Clean up
TRUNCATE TABLE wallet_addresses;

-- Insert EVM Addresses (avoiding Contracts to prevents spam on free tier)
-- Using a standard user wallet
INSERT INTO wallet_addresses (address, network_type, standard)
VALUES 
    ('0x28C6c06298d514Db089934071355E5743bf21d60', 'evm', 'ERC20')
ON CONFLICT DO NOTHING;

-- Insert Sui Addresses
INSERT INTO wallet_addresses (address, network_type, standard)
VALUES 
    ('0x1911de2c6e5d7cfb3c7e4b80dc62e4eae6ce2c2903bae3b86be394fc3eb36f5d', 'sui', 'SUI')
ON CONFLICT DO NOTHING;

-- Insert Bitcoin Addresses
INSERT INTO wallet_addresses (address, network_type, standard)
VALUES 
    ('bc1qryhgpmfv03qjhhp2dj8nw8g4ewg08jzmgy3cyx', 'bitcoin', 'P2PKH')
ON CONFLICT DO NOTHING;

-- Bulk check
SELECT * FROM wallet_addresses;
