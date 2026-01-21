package domain

// Event represents an emitted blockchain event
type Event struct {
	EventType   EventType
	ChainID     string
	BlockNumber uint64
	Transaction *Transaction
	EmittedAt   uint64
	Metadata    map[string]any
}

type EventType string

const (
	EventTypeTransactionConfirmed EventType = "transaction_confirmed"
	EventTypeTransactionReverted  EventType = "transaction_reverted"
	EventTypeReorgDetected        EventType = "reorg_detected"
)
