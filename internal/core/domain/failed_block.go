package domain

// FailedBlock represents a block that failed processing
type FailedBlock struct {
	ID          string `json:"id"`
	ChainID     string `json:"chain_id"`
	BlockNumber uint64 `json:"block_number"`
	FailureType FailureType
	Error       string            `json:"error_msg"`
	RetryCount  int               `json:"retry_count"`
	Status      FailedBlockStatus `json:"status"`
	LastAttempt uint64
	CreatedAt   uint64
}

type FailedBlockStatus string

const (
	FailedBlockStatusPending  FailedBlockStatus = "pending"
	FailedBlockStatusResolved FailedBlockStatus = "resolved"
	FailedBlockStatusIgnored  FailedBlockStatus = "ignored"
)

type FailureType string

const (
	FailureTypeRPC       FailureType = "rpc"
	FailureTypeParsing   FailureType = "parsing"
	FailureTypeDatabase  FailureType = "database"
	FailureTypeEmitter   FailureType = "emitter"
	FailureTypePermanent FailureType = "permanent"
)
