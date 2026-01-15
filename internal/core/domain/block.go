package domain

import "time"

// Block represents a blockchain block
type Block struct {
	ChainID    string
	Number     uint64
	Hash       string
	ParentHash string
	Timestamp  time.Time
	TxCount    int
	Metadata   map[string]any
	Status     BlockStatus
}

type BlockStatus string

const (
	BlockStatusPending   BlockStatus = "pending"
	BlockStatusProcessed BlockStatus = "processed"
	BlockStatusOrphaned  BlockStatus = "orphaned"
	BlockStatusFailed    BlockStatus = "failed"
)

// MissingBlock represents a gap in indexed blocks
type MissingBlock struct {
	ID          string
	ChainID     string
	FromBlock   uint64
	ToBlock     uint64
	Status      MissingBlockStatus
	RetryCount  int
	LastAttempt time.Time
	Priority    int
	CreatedAt   time.Time
}

type MissingBlockStatus string

const (
	MissingBlockStatusPending    MissingBlockStatus = "pending"
	MissingBlockStatusProcessing MissingBlockStatus = "processing"
	MissingBlockStatusCompleted  MissingBlockStatus = "completed"
	MissingBlockStatusFailed     MissingBlockStatus = "failed"
)

// FailedBlock represents a block that failed processing
type FailedBlock struct {
	ID          string
	ChainID     string
	BlockNumber uint64
	FailureType FailureType
	Error       string
	RetryCount  int
	Status      FailedBlockStatus
	LastAttempt time.Time
	CreatedAt   time.Time
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
