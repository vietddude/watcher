package domain

type CursorState string

const (
	CursorStateInit     CursorState = "init"
	CursorStateScanning CursorState = "scanning"
	CursorStateCatchup  CursorState = "catchup"
	CursorStatePaused   CursorState = "paused"
	CursorStateReorg    CursorState = "reorg"
)

// Cursor tracks indexing progress for a chain
type Cursor struct {
	ChainID     ChainID
	BlockNumber uint64
	BlockHash   string
	State       CursorState
}
