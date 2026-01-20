package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/vietddude/watcher/internal/core/config"
	"github.com/vietddude/watcher/internal/infra/storage"
)

// Pruner deletes old data based on retention policy.
type Pruner struct {
	cfg       config.ChainConfig
	blockRepo storage.BlockRepository
	txRepo    storage.TransactionRepository
}

// NewPruner creates a new Pruner worker.
func NewPruner(
	cfg config.ChainConfig,
	blockRepo storage.BlockRepository,
	txRepo storage.TransactionRepository,
) *Pruner {
	return &Pruner{
		cfg:       cfg,
		blockRepo: blockRepo,
		txRepo:    txRepo,
	}
}

// Start runs the pruner loop.
func (p *Pruner) Start(ctx context.Context) {
	if p.cfg.RetentionPeriod <= 0 {
		return // Retention disabled
	}

	// Calculate check interval (e.g., 10% of retention period, but max 1 hour)
	interval := min(p.cfg.RetentionPeriod/10, 1*time.Hour)
	interval = max(interval, 1*time.Minute)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial prune
	p.prune(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.prune(ctx)
		}
	}
}

func (p *Pruner) prune(ctx context.Context) {
	threshold := uint64(time.Now().Add(-p.cfg.RetentionPeriod).Unix())

	if err := p.blockRepo.DeleteBlocksOlderThan(ctx, p.cfg.ChainID, threshold); err != nil {
		// Log error (we don't have logger here, maybe fmt?)
		slog.Error("[Pruner] failed to prune blocks for %s: %v\n", p.cfg.ChainID, err)
	}

	if err := p.txRepo.DeleteTransactionsOlderThan(ctx, p.cfg.ChainID, threshold); err != nil {
		slog.Error("[Pruner] failed to prune transactions for %s: %v\n", p.cfg.ChainID, err)
	}
}
