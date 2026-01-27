package domain

type TxStatus string

const (
	TxStatusSuccess  TxStatus = "success"
	TxStatusFailed   TxStatus = "failed"
	TxStatusReverted TxStatus = "reverted"
	TxStatusInvalid  TxStatus = "invalid"
)

type TxType string

const (
	TxTypeNative TxType = "native"
	TxTypeERC20  TxType = "erc20"
	TxTypeTRC20  TxType = "trc20"
	TxTypeTRC10  TxType = "trc10"
	TxTypeToken  TxType = "token"
)

// Transaction represents a blockchain transaction
type Transaction struct {
	ChainID      ChainID
	BlockNumber  uint64
	BlockHash    string
	Hash         string
	Type         TxType
	TokenAddress string
	TokenAmount  string
	Index        int
	From         string
	To           string
	Value        string
	GasUsed      uint64
	GasPrice     string
	Status       TxStatus
	Timestamp    uint64 // unix timestamp
	RawData      []byte
}
