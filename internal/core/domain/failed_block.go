package domain

// FailedBlock represents a block that failed processing
type FailedBlock struct {
	ID          string
	ChainID     ChainID
	BlockNumber uint64
	FailureType FailureType
	Error       string
	RetryCount  int
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
