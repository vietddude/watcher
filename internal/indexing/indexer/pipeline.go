package indexer

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
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

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-p.stop:
			return nil
		case <-ticker.C:
			if err := p.processNextBlock(ctx); err != nil {
				// Log error (in real app)
				// Wait a bit before retrying loop to avoid tight loop on failure
				time.Sleep(time.Second)
			}
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
		return fmt.Errorf("failed to get cursor: %w", err)
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

	// 4. Fetch Transactions for block
	txs, err := p.cfg.ChainAdapter.GetTransactions(ctx, block)
	if err != nil {
		return p.handleError(ctx, targetBlockNum, fmt.Errorf("fetch txs failed: %w", err))
	}

	// 5. Filter Transactions
	// Adapter usually provides optimization for filtering
	// We need the list of addresses from filter first?
	// chain.Adapter.FilterTransactions takes []string addresses.
	// But filter.Filter stores them.
	// To use Adapter's optimized filtering, we need to extract addresses from Filter or let Adapter handle it.
	// Current Filter interface: Contains(), Add(), Size(). No "GetAddresses()".
	// Assuming linear scan using Filter.Contains for now if Adapter doesn't support generic Filter.
	// But wait, Adapter.FilterTransactions takes []string.
	// Ideally Filter interface should expose `GetInterestedAddresses()` or similar.
	// For simplicity in this iteration: We'll iterate manually using `Filter.Contains`.

	var relevantTxs []*domain.Transaction
	for _, tx := range txs {
		if p.cfg.Filter.Contains(tx.From) || p.cfg.Filter.Contains(tx.To) {
			relevantTxs = append(relevantTxs, tx)
		}
	}

	// Convert to Events
	var events []*domain.Event
	for _, tx := range relevantTxs {
		events = append(events, &domain.Event{
			ID:          tx.TxHash, // Use Hash as ID for now
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
