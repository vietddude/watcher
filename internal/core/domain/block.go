package domain

type BlockStatus string

const (
	BlockStatusPending   BlockStatus = "pending"
	BlockStatusProcessed BlockStatus = "processed"
	BlockStatusOrphaned  BlockStatus = "orphaned"
)

// Block represents a blockchain block
type Block struct {
	ChainID    ChainID
	Number     uint64
	Hash       string
	ParentHash string
	Timestamp  uint64
	Status     BlockStatus
}
