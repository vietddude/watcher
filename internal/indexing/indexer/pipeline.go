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

	// Check for empty filter
	if p.cfg.Filter.Size() == 0 {
		slog.Warn("Pipeline started with NO monitored addresses. Database will remain empty.", "chain", p.cfg.ChainID)
	}

	// Use dynamic timer if adaptive controller is available
	var timer *time.Timer
	if p.cfg.Controller != nil {
		timer = time.NewTimer(p.cfg.ScanInterval)
	} else {
		// Fallback to static ticker
		ticker := time.NewTicker(p.cfg.ScanInterval)
		defer ticker.Stop()
		return p.runWithStaticTicker(ctx, ticker)
	}
	defer timer.Stop()

	// Start background head monitor
	go p.monitorChainHead(ctx)

	// Process immediately on startup, then use dynamic timer
	for {
		shouldContinue, err := p.processNextBlock(ctx)
		if err != nil {
			slog.Error("Block processing error", "error", err)
			time.Sleep(time.Second)
		} else if shouldContinue {
			// Burst mode: don't wait for timer
			select {
			case <-ctx.Done():
				return nil
			case <-p.stop:
				return nil
			default:
				continue
			}
		}

		// Compute next interval based on current lag
		interval := p.computeNextInterval(ctx)
		timer.Reset(interval)

		select {
		case <-ctx.Done():
			return nil
		case <-p.stop:
			return nil
		case <-timer.C:
			// Continue to next iteration
		}
	}
}

// runWithStaticTicker is the fallback when adaptive controller is not available
func (p *Pipeline) runWithStaticTicker(ctx context.Context, ticker *time.Ticker) error {
	// Start background head monitor
	go p.monitorChainHead(ctx)

	// Process immediately on startup, then use ticker
	for {
		shouldContinue, err := p.processNextBlock(ctx)
		if err != nil {
			slog.Error("Block processing error", "error", err)
			time.Sleep(time.Second)
		} else if shouldContinue {
			// Burst mode: don't wait for ticker
			select {
			case <-ctx.Done():
				return nil
			case <-p.stop:
				return nil
			default:
				continue
			}
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

// computeNextInterval calculates the next scan interval based on current lag
func (p *Pipeline) computeNextInterval(ctx context.Context) time.Duration {
	chainName, _ := domain.ChainNameFromID(p.cfg.ChainID)
	if p.cfg.Controller == nil {
		metrics.AdaptiveBatchSize.WithLabelValues(chainName).Set(float64(p.cfg.BatchSize))
		return p.cfg.ScanInterval
	}

	// Get current cursor
	cursor, err := p.cfg.Cursor.Get(ctx, p.cfg.ChainID)
	if err != nil || cursor == nil {
		metrics.AdaptiveBatchSize.WithLabelValues(chainName).Set(float64(p.cfg.BatchSize))
		return p.cfg.ScanInterval
	}

	// Get latest block (use cache if available)
	var latestBlock uint64
	if p.cfg.HeadCache != nil {
		latestBlock, err = p.cfg.HeadCache.GetLatestBlock(ctx)
	} else {
		latestBlock, err = p.cfg.ChainAdapter.GetLatestBlock(ctx)
	}
	if err != nil {
		return p.cfg.ScanInterval
	}

	// Compute lag
	lag := int64(latestBlock) - int64(cursor.BlockNumber)

	// Compute optimal interval
	interval := p.cfg.Controller.ComputeInterval(lag)

	// Compute optimal batch size (pass 0 latency for now as it's not tracked in pipeline)
	batchSize := p.cfg.Controller.ComputeBatchSize(lag, 0)

	// Update metrics
	metrics.AdaptiveScanIntervalSeconds.WithLabelValues(chainName).Set(interval.Seconds())
	metrics.AdaptiveBatchSize.WithLabelValues(chainName).Set(float64(batchSize))
	metrics.ChainLag.WithLabelValues(chainName).Set(float64(lag))

	return interval
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
// Returns true if we should process the next block immediately (behind head)
func (p *Pipeline) processNextBlock(ctx context.Context) (bool, error) {
	// 1. Get current position
	cursor, err := p.cfg.Cursor.Get(ctx, p.cfg.ChainID)
	if err != nil {
		return false, fmt.Errorf("failed to get cursor: %w", err)
	}

	if cursor == nil {
		// Cursor not found - initialize at block 0 (or latest - finality in production)
		slog.Info("Cursor not found, initializing...")
		latestBlock, latestErr := p.cfg.ChainAdapter.GetLatestBlock(ctx)
		if latestErr != nil {
			return false, fmt.Errorf("failed to get latest block for initialization: %w", latestErr)
		}
		// Start from latest block minus some buffer to avoid missing recent blocks
		startBlock := latestBlock
		if startBlock > 10 {
			startBlock = latestBlock - 10
		}
		cursor, err = p.cfg.Cursor.Initialize(ctx, p.cfg.ChainID, startBlock)
		if err != nil {
			return false, fmt.Errorf("failed to initialize cursor: %w", err)
		}
		slog.Info("Cursor initialized", "startBlock", startBlock)
	}

	targetBlockNum := cursor.BlockNumber + 1

	// [NEW] Check for large lag and jump to head if necessary
	if p.cfg.GapJumpThreshold > 0 && p.cfg.MissingRepo != nil {
		latestBlock, err := p.getLatestBlock(ctx)
		if err == nil {
			lag := int64(latestBlock) - int64(cursor.BlockNumber)
			if lag > int64(p.cfg.GapJumpThreshold) {
				slog.Warn(
					"Large lag detected, jumping to head",
					"chain", p.cfg.ChainID,
					"lag", lag,
					"threshold", p.cfg.GapJumpThreshold,
					"current", cursor.BlockNumber,
					"target", latestBlock,
				)
				if err := p.jumpToHead(ctx, cursor.BlockNumber+1, latestBlock); err != nil {
					slog.Error("Failed to jump to head", "error", err)
				} else {
					// Return immediately to start processing from new head
					return true, nil
				}
			}
		}
	}

	// 2. Fetch block data (Adapter provides domain.Block)
	block, err := p.cfg.ChainAdapter.GetBlock(ctx, targetBlockNum)
	if err != nil {
		// Recovery: Handle transient/permanent errors
		return false, p.handleError(ctx, targetBlockNum, err)
	}
	if block == nil {
		// No new block yet (Adapter returns nil for future block)
		return false, nil
	}

	// 3. Reorg Detection
	// Check if this new block's parent matches our local history
	reorgInfo, err := p.cfg.Reorg.CheckParentHash(
		ctx,
		p.cfg.ChainID,
		targetBlockNum,
		block.ParentHash,
	)
	if err != nil {
		return false, p.handleError(ctx, targetBlockNum, fmt.Errorf("reorg check failed: %w", err))
	}

	if reorgInfo.Detected {
		// Handle Reorg!
		if _, err := p.cfg.ReorgHandler.Rollback(ctx, p.cfg.ChainID, reorgInfo.FromBlock, reorgInfo.SafeBlock, reorgInfo.SafeHash); err != nil {
			return false, fmt.Errorf("rollback failed: %w", err)
		}
		// Return to loop to pick up from safe block
		return true, nil // Retry immediately
	}

	// 4. Optimization: Check PreFilter if supported
	var txs []*domain.Transaction
	var skipFetch bool

	if preFilter, ok := p.cfg.ChainAdapter.(chain.PreFilterAdapter); ok {
		addrs := p.cfg.Filter.Addresses()
		if len(addrs) > 0 {
			relevant, err := preFilter.HasRelevantTransactions(ctx, block, addrs)
			if err != nil {
				slog.Warn("PreFilter check failed", "chain", p.cfg.ChainID, "block", targetBlockNum, "error", err)
			} else if !relevant {
				slog.Debug("Skipping block based on pre-filter", "chain", p.cfg.ChainID, "block", targetBlockNum)
				skipFetch = true
			}
		}
	}

	if !skipFetch {
		// Fetch Transactions for block
		var err error
		txs, err = p.cfg.ChainAdapter.GetTransactions(ctx, block)
		if err != nil {
			return false, p.handleError(
				ctx,
				targetBlockNum,
				fmt.Errorf("fetch txs failed: %w", err),
			)
		}
	}

	// Record metrics
	metrics.BlocksProcessed.WithLabelValues(string(p.cfg.ChainID)).Inc()
	metrics.IndexerLatestBlock.WithLabelValues(string(p.cfg.ChainID)).Set(float64(targetBlockNum))

	// 5. Filter Transactions using chain-specific logic
	relevantTxs, err := p.cfg.ChainAdapter.FilterTransactions(ctx, txs, p.cfg.Filter.Addresses())
	if err != nil {
		return false, p.handleError(ctx, targetBlockNum, fmt.Errorf("filter txs failed: %w", err))
	}

	slog.Debug(
		"Filter stats",
		"chain", p.cfg.ChainID,
		"block", targetBlockNum,
		"total_fetched", len(txs),
		"monitored_addrs", len(p.cfg.Filter.Addresses()),
		"relevant_count", len(relevantTxs),
	)

	// Metrics
	relevantCount := float64(len(relevantTxs))
	filteredCount := float64(len(txs) - len(relevantTxs))
	metrics.TransactionsProcessed.WithLabelValues(string(p.cfg.ChainID)).Add(relevantCount)
	metrics.TransactionsFiltered.WithLabelValues(string(p.cfg.ChainID)).Add(filteredCount)

	// 6. Enrich matched transactions with receipt data (gas used, status)
	if batchAdapter, ok := p.cfg.ChainAdapter.(chain.BatchEnrichAdapter); ok {
		// Use optimized batch enrichment
		if err := batchAdapter.EnrichTransactions(ctx, relevantTxs); err != nil {
			slog.Warn("Failed to batch enrich transactions", "error", err)
		}
	} else {
		// Fallback to sequential
		for _, tx := range relevantTxs {
			if err := p.cfg.ChainAdapter.EnrichTransaction(ctx, tx); err != nil {
				slog.Warn("Failed to enrich transaction", "tx", tx.Hash, "error", err)
			}
		}
	}

	// Log if we found relevant transactions
	if len(relevantTxs) > 0 {
		slog.Info(
			"Found relevant transactions",
			"chain",
			p.cfg.ChainID,
			"block",
			block.Number,
			"matched",
			len(relevantTxs),
			"total",
			len(txs),
		)
	}

	// Convert to Events
	var events []*domain.Event
	for _, tx := range relevantTxs {
		events = append(events, &domain.Event{
			EventType:   domain.EventTypeTransactionConfirmed,
			ChainID:     p.cfg.ChainID,
			BlockNumber: block.Number,
			Transaction: tx,
			EmittedAt:   uint64(time.Now().Unix()),
		})
	}

	// Emit events (FinalityBuffer handles buffering)
	if len(events) > 0 {
		if err := p.cfg.Emitter.EmitBatch(ctx, events); err != nil {
			return false, p.handleError(ctx, targetBlockNum, fmt.Errorf("emit failed: %w", err))
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

	// 7. Persist Data (Atomic Transaction)
	block.ChainID = p.cfg.ChainID // Ensure ChainID matches pipeline config
	block.Status = domain.BlockStatusProcessed

	// Set ChainID on all transactions
	for _, tx := range relevantTxs {
		tx.ChainID = p.cfg.ChainID
	}

	// Use UnitOfWork for atomic persistence if DB is available
	if p.cfg.DB != nil {
		uow, err := p.cfg.DB.NewUnitOfWork(ctx)
		if err != nil {
			return false, p.handleError(
				ctx,
				targetBlockNum,
				fmt.Errorf("failed to create unit of work: %w", err),
			)
		}
		defer uow.Rollback()

		// Save block with status already set to processed
		if err = uow.SaveBlocks(ctx, []*domain.Block{block}); err != nil {
			return false, p.handleError(
				ctx,
				targetBlockNum,
				fmt.Errorf("save block failed: %w", err),
			)
		}

		// Save transactions
		if len(relevantTxs) > 0 {
			if err = uow.SaveTransactions(ctx, relevantTxs); err != nil {
				return false, p.handleError(
					ctx,
					targetBlockNum,
					fmt.Errorf("save transactions failed: %w", err),
				)
			}
			slog.Debug("Transactions pending commit", "chain", p.cfg.ChainID, "count", len(relevantTxs))
		}

		// Advance cursor
		if err = uow.AdvanceCursor(ctx, p.cfg.ChainID, targetBlockNum, block.Hash); err != nil {
			return false, p.handleError(
				ctx,
				targetBlockNum,
				fmt.Errorf("cursor advance failed: %w", err),
			)
		}

		// Commit all changes atomically
		if err = uow.Commit(); err != nil {
			return false, p.handleError(ctx, targetBlockNum, fmt.Errorf("commit failed: %w", err))
		}

		// Track successful atomic commit
		chainName, _ := domain.ChainNameFromID(p.cfg.ChainID)
		metrics.DBAtomicCommits.WithLabelValues(chainName).Inc()
	} else {
		// Fallback to non-atomic persistence for backward compatibility
		if err = p.cfg.BlockRepo.Save(ctx, block); err != nil {
			return false, p.handleError(ctx, targetBlockNum, fmt.Errorf("save block failed: %w", err))
		}
		if err := p.cfg.BlockRepo.UpdateStatus(ctx, p.cfg.ChainID, block.Number, domain.BlockStatusProcessed); err != nil {
			slog.Warn("Failed to update block status", "block", block.Number, "error", err)
		}
		if len(relevantTxs) > 0 {
			if err = p.cfg.TransactionRepo.SaveBatch(ctx, relevantTxs); err != nil {
				return false, p.handleError(ctx, targetBlockNum, fmt.Errorf("save transactions failed: %w", err))
			}
		}
		if err := p.cfg.Cursor.Advance(ctx, p.cfg.ChainID, targetBlockNum, block.Hash); err != nil {
			return false, fmt.Errorf("cursor advance failed: %w", err)
		}
	}

	// Optimization: If we just processed a block successfully,
	// and we know there are more blocks (e.g. lag > 0), return true to skip sleep.
	// For now, we return true to indicate "check again immediately".
	// The caller loop will enforce sleep if we hit head (GetBlock returns nil or error).
	return true, nil
}

func (p *Pipeline) handleError(ctx context.Context, blockNum uint64, err error) error {
	// Delegate to recovery handler
	if p.cfg.Recovery != nil {
		return p.cfg.Recovery.HandleFailure(ctx, p.cfg.ChainID, blockNum, err)
	}
	return err
}

func (p *Pipeline) monitorChainHead(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stop:
			return
		case <-ticker.C:
			latest, err := p.cfg.ChainAdapter.GetLatestBlock(ctx)
			if err != nil {
				slog.Error("Failed to fetch chain head", "chain", p.cfg.ChainID, "error", err)
				continue
			}
			metrics.ChainLatestBlock.WithLabelValues(string(p.cfg.ChainID)).Set(float64(latest))
		}
	}
}
