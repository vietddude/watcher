package domain

// MissingBlock represents a gap in indexed blocks
type MissingBlock struct {
	ID          string
	ChainID     ChainID
	FromBlock   uint64
	ToBlock     uint64
	Status      MissingBlockStatus
	RetryCount  int
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
