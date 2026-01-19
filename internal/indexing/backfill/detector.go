package backfill

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/storage"
)

// Detector finds gaps without making RPC calls.
// Uses cursor errors and database queries only.
type Detector struct {
	blockRepo   storage.BlockRepository
	missingRepo storage.MissingBlockRepository
}

// OnCursorGap handles a gap detected by cursor.Advance().
// This is called when cursor returns ErrBlockGap.
// It queues the missing blocks without making any RPC calls.
func (d *Detector) OnCursorGap(
	ctx context.Context,
	chainID string,
	currentBlock, newBlock uint64,
) error {
	if newBlock <= currentBlock+1 {
		return nil // No gap
	}

	gap := Gap{
		FromBlock: currentBlock + 1,
		ToBlock:   newBlock - 1,
	}

	return d.queueGap(ctx, chainID, gap, priorityHigh)
}

// ScanDatabase finds gaps in stored blocks using BlockRepository.FindGaps().
// This is a periodic scan that doesn't make RPC calls.
func (d *Detector) ScanDatabase(
	ctx context.Context,
	chainID string,
	fromBlock, toBlock uint64,
) ([]Gap, error) {
	dbGaps, err := d.blockRepo.FindGaps(ctx, chainID, fromBlock, toBlock)
	if err != nil {
		return nil, fmt.Errorf("failed to find gaps: %w", err)
	}

	gaps := make([]Gap, len(dbGaps))
	for i, g := range dbGaps {
		gaps[i] = Gap{
			FromBlock: g.FromBlock,
			ToBlock:   g.ToBlock,
		}
	}

	return gaps, nil
}

// QueueGapsFromScan queues all gaps found by ScanDatabase.
func (d *Detector) QueueGapsFromScan(ctx context.Context, chainID string, gaps []Gap) error {
	for _, gap := range gaps {
		if err := d.queueGap(ctx, chainID, gap, priorityLow); err != nil {
			return err
		}
	}
	return nil
}

// Priority levels for missing blocks
const (
	priorityHigh = 10 // Recent gaps (from cursor)
	priorityLow  = 1  // Historical gaps (from scan)
)

func (d *Detector) queueGap(ctx context.Context, chainID string, gap Gap, priority int) error {
	missing := &domain.MissingBlock{
		ID:        uuid.New().String(),
		ChainID:   chainID,
		FromBlock: gap.FromBlock,
		ToBlock:   gap.ToBlock,
		Status:    domain.MissingBlockStatusPending,
		Priority:  priority,
		CreatedAt: uint64(time.Now().Unix()),
	}

	return d.missingRepo.Add(ctx, missing)
}
