package indexer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/indexing/metrics"
)

// getLatestBlock returns the latest block number, using cache if available
func (p *Pipeline) getLatestBlock(ctx context.Context) (uint64, error) {
	if p.cfg.HeadCache != nil {
		if cached, err := p.cfg.HeadCache.GetLatestBlock(ctx); err == nil && cached > 0 {
			return cached, nil
		}
	}
	return p.cfg.ChainAdapter.GetLatestBlock(ctx)
}

// jumpToHead creates a missing block entry for the gap and moves the cursor to the head
func (p *Pipeline) jumpToHead(ctx context.Context, fromBlock, toBlock uint64) error {
	// 1. Calculate gap range
	// We want to jump to toBlock (latest), so the gap is from...toBlock-1
	// The cursor will be set to toBlock, so next process is toBlock+1?
	// Wait, if toBlock is 100, we want to start processing 101?
	// Usually "latest" means the block that just finished.
	// If we set cursor to 'latest', then target is latest+1.
	// So gap is [fromBlock, latest].

	// Create missing block for the gap
	gap := &domain.MissingBlock{
		ChainID:    p.cfg.ChainID,
		FromBlock:  fromBlock,
		ToBlock:    toBlock,
		Status:     domain.MissingBlockStatusPending,
		RetryCount: 0,
		CreatedAt:  uint64(time.Now().Unix()),
	}

	// Check if we should split large gaps?
	// For now, just create one big gap, backfill processor handles it.

	if err := p.cfg.MissingRepo.Add(ctx, gap); err != nil {
		return fmt.Errorf("failed to add missing block gap: %w", err)
	}

	// 2. Advance cursor to toBlock
	// Use Jump() if available, otherwise fake an Advance
	// Cursor Manager has Jump()
	if err := p.cfg.Cursor.Jump(ctx, p.cfg.ChainID, toBlock); err != nil {
		return fmt.Errorf("failed to jump cursor: %w", err)
	}

	slog.Info("Jumped cursor to head", "chain", p.cfg.ChainID, "new_cursor", toBlock)

	// Record metrics
	metrics.GapJumpsTotal.WithLabelValues(string(p.cfg.ChainID)).Inc()
	metrics.GapJumpSize.WithLabelValues(string(p.cfg.ChainID)).Observe(float64(toBlock - fromBlock))

	return nil
}
