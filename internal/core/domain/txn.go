package domain

type TxStatus string

const (
	TxStatusSuccess  TxStatus = "success"
	TxStatusFailed   TxStatus = "failed"
	TxStatusReverted TxStatus = "reverted"
	TxStatusInvalid  TxStatus = "invalid"
)

// Transaction represents a blockchain transaction
type Transaction struct {
	ChainID     ChainID
	BlockNumber uint64
	BlockHash   string
	Hash        string
	Index       int
	From        string
	To          string
	Value       string
	GasUsed     uint64
	GasPrice    string
	Status      TxStatus
	Timestamp   uint64 // unix timestamp
	RawData     []byte
}
