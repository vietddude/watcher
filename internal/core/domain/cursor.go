package domain

// Cursor represents the indexing position
type Cursor struct {
	ID               string         `json:"id"`
	ChainID          string         `json:"chain_id"`
	CurrentBlock     uint64         `json:"block_number"`
	CurrentBlockHash string         `json:"block_hash"`
	UpdatedAt        uint64         `json:"updated_at"`
	State            CursorState    `json:"state"`
	Metadata         map[string]any `json:"metadata"`
}

type CursorState string

const (
	CursorStateInit     CursorState = "init"
	CursorStateScanning CursorState = "scanning"
	CursorStateCatchup  CursorState = "catchup"
	CursorStateBackfill CursorState = "backfill"
	CursorStatePaused   CursorState = "paused"
	CursorStateReorg    CursorState = "reorg"
)
