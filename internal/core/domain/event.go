package domain

import "time"

// Event represents an emitted blockchain event
type Event struct {
	ID          string
	EventType   EventType
	ChainID     string
	BlockNumber uint64
	Transaction *Transaction
	EmittedAt   time.Time
	Metadata    map[string]any
}

type EventType string

const (
	EventTypeTransactionConfirmed EventType = "transaction_confirmed"
	EventTypeTransactionReverted  EventType = "transaction_reverted"
	EventTypeReorgDetected        EventType = "reorg_detected"
)
