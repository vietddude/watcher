package domain

import "time"

// Transaction represents a blockchain transaction
type Transaction struct {
	ChainID     string
	BlockNumber uint64
	BlockHash   string
	TxHash      string
	TxIndex     int
	From        string
	To          string
	Value       string
	GasUsed     uint64
	GasPrice    string
	Status      TxStatus
	Timestamp   time.Time
	RawData     []byte
}

type TxStatus string

const (
	TxStatusSuccess  TxStatus = "success"
	TxStatusFailed   TxStatus = "failed"
	TxStatusInvalid  TxStatus = "invalid"
	TxStatusReverted TxStatus = "reverted"
)
