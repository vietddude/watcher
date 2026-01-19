package domain

// Transaction represents a blockchain transaction
type Transaction struct {
	ID          uint64   `json:"id"`
	ChainID     string   `json:"chain_id"`
	BlockNumber uint64   `json:"block_number"`
	BlockHash   string   `json:"block_hash"`
	TxHash      string   `json:"tx_hash"`
	TxIndex     int      `json:"tx_index"`
	From        string   `json:"from_address"`
	To          string   `json:"to_address"`
	Value       string   `json:"value"`
	GasUsed     uint64   `json:"gas_used"`
	GasPrice    string   `json:"gas_price"`
	Status      TxStatus `json:"status"`
	Timestamp   uint64   `json:"timestamp"`
	RawData     []byte   `json:"raw_data"`
}

type TxStatus string

const (
	TxStatusSuccess  TxStatus = "success"
	TxStatusFailed   TxStatus = "failed"
	TxStatusInvalid  TxStatus = "invalid"
	TxStatusReverted TxStatus = "reverted"
)
