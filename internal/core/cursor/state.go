package cursor

import (
	"errors"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
)

// State is an alias for domain.CursorState for internal use.
type State = domain.CursorState

// ErrInvalidTransition is returned when an invalid state transition is attempted.
var ErrInvalidTransition = errors.New("invalid state transition")

// ValidTransitions defines allowed state transitions.
// Key is the current state, value is the list of valid next states.
var ValidTransitions = map[State][]State{
	domain.CursorStateInit: {domain.CursorStateScanning, domain.CursorStateCatchup},
	domain.CursorStateScanning: {
		domain.CursorStateCatchup,
		domain.CursorStateBackfill,
		domain.CursorStatePaused,
		domain.CursorStateReorg,
	},
	domain.CursorStateCatchup: {
		domain.CursorStateScanning,
		domain.CursorStateBackfill,
		domain.CursorStatePaused,
		domain.CursorStateReorg,
	},
	domain.CursorStateBackfill: {domain.CursorStateScanning, domain.CursorStatePaused},
	domain.CursorStatePaused: {
		domain.CursorStateScanning,
		domain.CursorStateCatchup,
		domain.CursorStateBackfill,
	},
	domain.CursorStateReorg: {domain.CursorStateScanning, domain.CursorStateCatchup},
}

// CanTransition checks if a transition from one state to another is valid.
func CanTransition(from, to State) bool {
	validTargets, ok := ValidTransitions[from]
	if !ok {
		return false
	}

	for _, target := range validTargets {
		if target == to {
			return true
		}
	}
	return false
}

// Transition represents a state change with metadata.
type Transition struct {
	From      State
	To        State
	Reason    string
	Timestamp time.Time
}

// NewTransition creates a new transition record.
func NewTransition(from, to State, reason string) Transition {
	return Transition{
		From:      from,
		To:        to,
		Reason:    reason,
		Timestamp: time.Now(),
	}
}

// IsValid returns true if this transition is allowed by the state machine.
func (t Transition) IsValid() bool {
	return CanTransition(t.From, t.To)
}

// StateDescription returns a human-readable description of a state.
func StateDescription(s State) string {
	switch s {
	case domain.CursorStateInit:
		return "Initializing - cursor created, not yet started"
	case domain.CursorStateScanning:
		return "Scanning - normal forward indexing"
	case domain.CursorStateCatchup:
		return "Catching up - behind chain tip, moving fast"
	case domain.CursorStateBackfill:
		return "Backfilling - processing historical gaps"
	case domain.CursorStatePaused:
		return "Paused - stopped by operator"
	case domain.CursorStateReorg:
		return "Reorg - rolling back due to chain reorganization"
	default:
		return "Unknown state"
	}
}
