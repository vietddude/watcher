package domain

// Block represents a blockchain block
type Block struct {
	ID         string         `json:"id"`
	ChainID    string         `json:"chain_id"`
	Number     uint64         `json:"block_number"`
	Hash       string         `json:"block_hash"`
	ParentHash string         `json:"parent_hash"`
	Timestamp  uint64         `json:"block_timestamp"`
	TxCount    int            `json:"tx_count"`
	Metadata   map[string]any `json:"metadata"`
	Status     BlockStatus    `json:"status"`
	UpdatedAt  uint64         `json:"updated_at"`
	CreatedAt  uint64         `json:"created_at"`
}

type BlockStatus string

const (
	BlockStatusPending   BlockStatus = "pending"
	BlockStatusProcessed BlockStatus = "processed"
	BlockStatusOrphaned  BlockStatus = "orphaned"
	BlockStatusFailed    BlockStatus = "failed"
)
