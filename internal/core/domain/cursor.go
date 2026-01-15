package domain

import "time"

// Cursor represents the indexing position
type Cursor struct {
	ChainID          string
	CurrentBlock     uint64
	CurrentBlockHash string
	LastUpdated      time.Time
	State            CursorState
	Metadata         map[string]interface{}
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
