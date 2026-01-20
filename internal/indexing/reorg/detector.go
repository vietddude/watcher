package reorg

import (
	"context"
	"fmt"

	"github.com/vietddude/watcher/internal/infra/storage"
)

// Detector checks for chain reorganizations using parent hash verification.
type Detector struct {
	config    Config
	blockRepo storage.BlockRepository
}

// ReorgInfo contains information about a detected reorganization.
type ReorgInfo struct {
	Detected  bool
	Depth     int
	FromBlock uint64 // First orphaned block
	SafeBlock uint64 // Last valid block
	SafeHash  string
}

// CheckParentHash verifies new block's parent hash matches stored block.
// This uses data already fetched (0 extra RPC calls).
//
// Returns ReorgInfo with Detected=true if reorg found.
func (d *Detector) CheckParentHash(
	ctx context.Context,
	chainID string,
	newBlockNum uint64,
	parentHash string,
) (*ReorgInfo, error) {
	if newBlockNum == 0 {
		return &ReorgInfo{Detected: false}, nil
	}

	prevBlockNum := newBlockNum - 1
	storedBlock, err := d.blockRepo.GetByNumber(ctx, chainID, prevBlockNum)
	if err != nil {
		return nil, fmt.Errorf("failed to get block %d: %w", prevBlockNum, err)
	}

	// Block not in DB yet - no reorg (we're ahead)
	if storedBlock == nil {
		return &ReorgInfo{Detected: false}, nil
	}

	// Parent hash matches - no reorg
	if storedBlock.Hash == parentHash {
		return &ReorgInfo{Detected: false}, nil
	}

	// Mismatch! Reorg detected
	// Find safe point by walking backwards
	safeBlock, safeHash, depth, err := d.findSafePoint(ctx, chainID, prevBlockNum)
	if err != nil {
		return nil, fmt.Errorf("failed to find safe point: %w", err)
	}

	return &ReorgInfo{
		Detected:  true,
		Depth:     depth,
		FromBlock: safeBlock + 1,
		SafeBlock: safeBlock,
		SafeHash:  safeHash,
	}, nil
}

// findSafePoint walks backwards to find the last matching block.
// Uses only stored data (no RPC calls).
func (d *Detector) findSafePoint(
	ctx context.Context,
	chainID string,
	fromBlock uint64,
) (safeBlock uint64, safeHash string, depth int, err error) {
	// Start from the mismatched block and go backwards
	currentBlock := fromBlock
	depth = 1

	for currentBlock > 0 {
		// Get parent of the orphaned block from our DB
		block, err := d.blockRepo.GetByNumber(ctx, chainID, currentBlock)
		if err != nil {
			return 0, "", 0, fmt.Errorf("failed to get block %d: %w", currentBlock, err)
		}
		if block == nil {
			// Reached beginning of our indexed data
			return currentBlock, "", depth, nil
		}

		// Check if parent of current block matches what we have stored
		parentBlock, err := d.blockRepo.GetByNumber(ctx, chainID, currentBlock-1)
		if err != nil {
			return 0, "", 0, fmt.Errorf("failed to get block %d: %w", currentBlock-1, err)
		}
		if parentBlock == nil {
			// No more stored blocks, this is our safe point
			return currentBlock - 1, "", depth, nil
		}

		// If the stored parent hash matches parent block's hash, we found safe point
		if block.ParentHash == parentBlock.Hash {
			return parentBlock.Number, parentBlock.Hash, depth, nil
		}

		currentBlock--
		depth++

		// Safety limit to prevent infinite loops
		maxDepth := d.config.MaxDepth
		if maxDepth == 0 {
			maxDepth = 100 // Default
		}
		if depth > maxDepth {
			return 0, "", 0, fmt.Errorf("reorg depth exceeds %d blocks", maxDepth)
		}
	}

	return 0, "", depth, nil
}

// VerifyWithRPC verifies block hash by fetching from RPC.
// Only use this when you need to confirm a reorg.
func (d *Detector) VerifyWithRPC(
	ctx context.Context,
	chainID string,
	blockNum uint64,
	fetcher HashFetcher,
) (bool, error) {
	storedBlock, err := d.blockRepo.GetByNumber(ctx, chainID, blockNum)
	if err != nil {
		return false, fmt.Errorf("failed to get stored block: %w", err)
	}
	if storedBlock == nil {
		return true, nil // No stored block, nothing to verify
	}

	rpcHash, err := fetcher(chainID, blockNum)
	if err != nil {
		return false, fmt.Errorf("failed to fetch hash from RPC: %w", err)
	}

	return storedBlock.Hash == rpcHash, nil
}
