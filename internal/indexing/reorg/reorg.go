// Package reorg handles blockchain reorganization detection and recovery.
//
// # Design: RPC-Minimal Detection
//
// Detection uses parent hash verification (0 extra RPC calls):
//   - When fetching block N, its parent_hash is already available
//   - Compare with stored block N-1 hash
//   - If mismatch, reorg detected
//
// # Rollback Process
//
//  1. Detect reorg via parent hash mismatch
//  2. Find safe point by walking backwards
//  3. Mark orphaned blocks and transactions
//  4. Emit revert events for downstream services
//  5. Reset cursor to safe point
//
// # Usage
//
//	detector := reorg.NewDetector(blockRepo)
//	handler := reorg.NewHandler(blockRepo, txRepo, cursorMgr)
//
//	// Check on every new block fetch
//	if depth, safe, _ := detector.CheckParentHash(ctx, chainID, blockNum, parentHash); depth > 0 {
//	    handler.Rollback(ctx, chainID, blockNum, safe)
//	}
package reorg

import (
	"github.com/vietddude/watcher/internal/core/cursor"
	"github.com/vietddude/watcher/internal/infra/storage"
)

// HashFetcher fetches block hash from RPC (used when finding safe point).
type HashFetcher func(chainID string, blockNum uint64) (hash string, err error)

// Config holds configuration for reorg detection.
type Config struct {
	MaxDepth int // Maximum depth to search for a safe point (default: 100)
}

// NewDetector creates a new reorg detector.
func NewDetector(config Config, blockRepo storage.BlockRepository) *Detector {
	return &Detector{
		config:    config,
		blockRepo: blockRepo,
	}
}

// NewHandler creates a new reorg handler.
func NewHandler(blockRepo storage.BlockRepository, txRepo storage.TransactionRepository, cursorMgr cursor.Manager) *Handler {
	return &Handler{
		blockRepo: blockRepo,
		txRepo:    txRepo,
		cursorMgr: cursorMgr,
	}
}
