-- +goose Up
-- +goose StatementBegin
ALTER TABLE transactions 
ADD COLUMN tx_type VARCHAR(32) DEFAULT 'native',
ADD COLUMN token_address VARCHAR(128),
ADD COLUMN token_amount VARCHAR(128);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE transactions 
DROP COLUMN tx_type,
DROP COLUMN token_address,
DROP COLUMN token_amount;
-- +goose StatementEnd
