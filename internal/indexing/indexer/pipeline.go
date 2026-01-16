package indexer

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/indexing/metrics"
	"github.com/vietddude/watcher/internal/infra/chain"
)

// Pipeline implements the Indexer interface
type Pipeline struct {
	cfg     Config
	running atomic.Bool
	stop    chan struct{}
}

// NewPipeline creates a new indexing pipeline
func NewPipeline(cfg Config) *Pipeline {
	return &Pipeline{
		cfg:  cfg,
		stop: make(chan struct{}),
	}
}

// Start begins the indexing loop
func (p *Pipeline) Start(ctx context.Context) error {
	if !p.running.CompareAndSwap(false, true) {
		return fmt.Errorf("pipeline already running")
	}
	defer p.running.Store(false)

	ticker := time.NewTicker(p.cfg.ScanInterval)
	defer ticker.Stop()

	// Process immediately on startup, then use ticker
	for {
		if err := p.processNextBlock(ctx); err != nil {
			slog.Error("Block processing error", "error", err)
			time.Sleep(time.Second)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-p.stop:
			return nil
		case <-ticker.C:
			// Continue to next iteration
		}
	}
}

// Stop stops the pipeline
func (p *Pipeline) Stop() error {
	if p.running.Load() {
		close(p.stop)
	}
	return nil
}

// GetStatus returns the current status
func (p *Pipeline) GetStatus() Status {
	// Snapshot current state from cursor
	// This is a simplified view - production would read from memory cache
	return Status{
		ChainID: p.cfg.ChainID,
		Running: p.running.Load(),
	}
}

// processNextBlock executes one step of the pipeline
func (p *Pipeline) processNextBlock(ctx context.Context) error {
	// 1. Get current position
	cursor, err := p.cfg.Cursor.Get(ctx, p.cfg.ChainID)
	if err != nil {
		// Cursor not found - initialize at block 0 (or latest - finality in production)
		slog.Info("Cursor not found, initializing...")
		latestBlock, latestErr := p.cfg.ChainAdapter.GetLatestBlock(ctx)
		if latestErr != nil {
			return fmt.Errorf("failed to get latest block for initialization: %w", latestErr)
		}
		// Start from latest block minus some buffer to avoid missing recent blocks
		startBlock := latestBlock
		if startBlock > 10 {
			startBlock = latestBlock - 10
		}
		cursor, err = p.cfg.Cursor.Initialize(ctx, p.cfg.ChainID, startBlock)
		if err != nil {
			return fmt.Errorf("failed to initialize cursor: %w", err)
		}
		slog.Info("Cursor initialized", "startBlock", startBlock)
	}

	targetBlockNum := cursor.CurrentBlock + 1

	// 2. Fetch block data (Adapter provides domain.Block)
	block, err := p.cfg.ChainAdapter.GetBlock(ctx, targetBlockNum)
	if err != nil {
		// Recovery: Handle transient/permanent errors
		return p.handleError(ctx, targetBlockNum, err)
	}
	if block == nil {
		// No new block yet (Adapter returns nil for future block)
		return nil
	}

	// 3. Reorg Detection
	// Check if this new block's parent matches our local history
	reorgInfo, err := p.cfg.Reorg.CheckParentHash(ctx, p.cfg.ChainID, targetBlockNum, block.ParentHash)
	if err != nil {
		return p.handleError(ctx, targetBlockNum, fmt.Errorf("reorg check failed: %w", err))
	}

	if reorgInfo.Detected {
		// Handle Reorg!
		if _, err := p.cfg.ReorgHandler.Rollback(ctx, p.cfg.ChainID, reorgInfo.FromBlock, reorgInfo.SafeBlock, reorgInfo.SafeHash); err != nil {
			return fmt.Errorf("rollback failed: %w", err)
		}
		// Return to loop to pick up from safe block
		return nil
	}

	// 4. Optimization: Check PreFilter if supported
	var txs []*domain.Transaction
	var skipFetch bool

	if preFilter, ok := p.cfg.ChainAdapter.(chain.PreFilterAdapter); ok {
		addrs := p.cfg.Filter.Addresses()
		if len(addrs) > 0 {
			relevant, err := preFilter.HasRelevantTransactions(ctx, block, addrs)
			if err != nil {
				slog.Warn("PreFilter check failed", "block", targetBlockNum, "error", err)
			} else if !relevant {
				slog.Debug("Skipping block based on pre-filter", "block", targetBlockNum)
				skipFetch = true
			}
		}
	}

	if !skipFetch {
		// Fetch Transactions for block
		var err error
		txs, err = p.cfg.ChainAdapter.GetTransactions(ctx, block)
		if err != nil {
			return p.handleError(ctx, targetBlockNum, fmt.Errorf("fetch txs failed: %w", err))
		}
	}

	// Log block processing
	label := "block"
	if p.cfg.ChainType == "sui" {
		label = "checkpoint"
	}
	slog.Info(fmt.Sprintf("Processing %s", label), "chain", p.cfg.ChainName, label, targetBlockNum, "hash", block.Hash[:16]+"...", "txs", len(txs))

	// Record metrics
	metrics.BlocksProcessed.WithLabelValues(p.cfg.ChainID).Inc()
	metrics.TransactionsProcessed.WithLabelValues(p.cfg.ChainID).Add(float64(len(txs)))

	// 5. Filter Transactions using bloom filter
	var relevantTxs []*domain.Transaction
	for _, tx := range txs {
		if p.cfg.Filter.Contains(tx.From) || p.cfg.Filter.Contains(tx.To) {
			relevantTxs = append(relevantTxs, tx)
		}
	}

	// 6. Enrich matched transactions with receipt data (gas used, status)
	for _, tx := range relevantTxs {
		if err := p.cfg.ChainAdapter.EnrichTransaction(ctx, tx); err != nil {
			slog.Warn("Failed to enrich transaction", "tx", tx.TxHash, "error", err)
		}
	}

	// Log if we found relevant transactions
	if len(relevantTxs) > 0 {
		slog.Info("Found relevant transactions", "block", block.Number, "matched", len(relevantTxs), "total", len(txs))
	}

	// Convert to Events
	var events []*domain.Event
	for _, tx := range relevantTxs {
		events = append(events, &domain.Event{
			ID:          tx.TxHash,
			EventType:   domain.EventTypeTransactionConfirmed,
			ChainID:     p.cfg.ChainID,
			BlockNumber: block.Number,
			Transaction: tx,
			EmittedAt:   time.Now(),
		})
	}

	// Emit events (FinalityBuffer handles buffering)
	if len(events) > 0 {
		if err := p.cfg.Emitter.EmitBatch(ctx, events); err != nil {
			return p.handleError(ctx, targetBlockNum, fmt.Errorf("emit failed: %w", err))
		}
	}

	// 6. Notify Buffer of new block height
	type BlockListener interface {
		OnNewBlock(ctx context.Context, height uint64) error
	}
	if listener, ok := p.cfg.Emitter.(BlockListener); ok {
		if err := listener.OnNewBlock(ctx, targetBlockNum); err != nil {
			// Log error but don't stop processing
			fmt.Printf("failed to notify emitter of new block: %v\n", err)
		}
	}

	// 7. Persist Data
	if err := p.cfg.BlockRepo.Save(ctx, block); err != nil {
		return p.handleError(ctx, targetBlockNum, fmt.Errorf("save block failed: %w", err))
	}
	// TODO: Save Transactions (Batch)

	// 8. Update Cursor
	// Advance cursor to this block
	if err := p.cfg.Cursor.Advance(ctx, p.cfg.ChainID, targetBlockNum, block.Hash); err != nil {
		return fmt.Errorf("cursor advance failed: %w", err)
	}

	return nil
}

func (p *Pipeline) handleError(ctx context.Context, blockNum uint64, err error) error {
	// Delegate to recovery handler
	if p.cfg.Recovery != nil {
		return p.cfg.Recovery.HandleFailure(ctx, p.cfg.ChainID, blockNum, err)
	}
	return err
}
