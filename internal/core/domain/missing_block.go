package domain

// MissingBlock represents a gap in indexed blocks
type MissingBlock struct {
	ID          string             `json:"id"`
	ChainID     string             `json:"chain_id"`
	FromBlock   uint64             `json:"from_block"`
	ToBlock     uint64             `json:"to_block"`
	Status      MissingBlockStatus `json:"status"`
	RetryCount  int                `json:"attempts"`
	LastAttempt uint64
	Priority    int
	CreatedAt   uint64
}

type MissingBlockStatus string

const (
	MissingBlockStatusPending    MissingBlockStatus = "pending"
	MissingBlockStatusProcessing MissingBlockStatus = "processing"
	MissingBlockStatusCompleted  MissingBlockStatus = "completed"
	MissingBlockStatusFailed     MissingBlockStatus = "failed"
)
