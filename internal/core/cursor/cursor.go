// Package cursor tracks the indexing position for each blockchain.
//
// # Purpose
//
// The cursor acts as a "bookmark" that remembers where the indexer is in each chain:
//   - Current block number: know which block to process next
//   - Block hash: detect chain reorganizations (if hash changes, reorg happened)
//   - State: control behavior (scanning, catching up, paused, handling reorg)
//
// # Key Features
//
// State Machine - Only allows valid transitions:
//
//	INIT → SCANNING → REORG → SCANNING (valid)
//	PAUSED → REORG (invalid - can't go to reorg from paused)
//
// Gap Detection - When you call Advance(1005) but cursor is at 1000,
// it returns ErrBlockGap so you know blocks 1001-1004 are missing.
//
// Atomic Updates - Cursor only advances AFTER a block is fully processed.
//
// Reorg Safety - Stores block hash with position to detect chain reorganizations.
//
// # Quick Start
//
//	manager := cursor.NewManager(cursorRepo)
//
//	// Initialize cursor at block 1000
//	c, _ := manager.Initialize(ctx, "ethereum", 1000)
//
//	// Start scanning
//	manager.SetState(ctx, "ethereum", cursor.StateScanning, "indexer started")
//
//	// Process blocks - must be sequential
//	manager.Advance(ctx, "ethereum", 1001, "0xabc...")  // ✓ OK
//	manager.Advance(ctx, "ethereum", 1005, "0xdef...")  // ✗ ErrBlockGap
//
//	// Handle reorg - rollback to safe block
//	manager.Rollback(ctx, "ethereum", 995, "0xsafe...")
//
//	// Track state changes
//	manager.SetStateChangeCallback(func(chainID string, t cursor.Transition) {
//	    log.Printf("Cursor %s: %s -> %s (%s)", chainID, t.From, t.To, t.Reason)
//	})
//
// # Package Structure
//
//   - state.go   - State machine definitions and valid transitions
//   - manager.go - Core Manager implementation with gap detection, rollback
//   - metrics.go - Performance metrics (blocks/sec, state history)
package cursor

import (
	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/storage"
)

// =============================================================================
// Re-exported types from domain package
// =============================================================================

// Cursor represents the indexing position for a chain.
type Cursor = domain.Cursor

// CursorState represents the current state of the cursor.
type CursorState = domain.CursorState

// State constants re-exported for convenience.
const (
	StateInit     = domain.CursorStateInit
	StateScanning = domain.CursorStateScanning
	StateCatchup  = domain.CursorStateCatchup
	StateBackfill = domain.CursorStateBackfill
	StatePaused   = domain.CursorStatePaused
	StateReorg    = domain.CursorStateReorg
)

// =============================================================================
// Constructor functions
// =============================================================================

// NewManager creates a new cursor manager with the given repository.
func NewManager(repo storage.CursorRepository) *DefaultManager {
	return &DefaultManager{
		repo:             repo,
		blockTimeHistory: make(map[string]*MetricsCollector),
	}
}

// NewMetricsCollector creates a new metrics collector with the given window size.
func NewMetricsCollector(windowSize int) *MetricsCollector {
	if windowSize <= 0 {
		windowSize = 100
	}
	return &MetricsCollector{
		windowSize:  windowSize,
		blockTimes:  make([]blockRecord, 0, windowSize),
		transitions: make([]Transition, 0, 10),
	}
}
