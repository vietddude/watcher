package backfill

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/vietddude/watcher/internal/indexing/metrics"
	"github.com/vietddude/watcher/internal/infra/rpc"
	"github.com/vietddude/watcher/internal/infra/storage"
)

var (
	// ErrQuotaExceeded is returned when RPC quota is too high.
	ErrQuotaExceeded = errors.New("RPC quota exceeded, backfill paused")

	// ErrNoMissingBlocks is returned when there are no blocks to process.
	ErrNoMissingBlocks = errors.New("no missing blocks to process")
)

// ProcessorConfig configures the processor rate limits.
type ProcessorConfig struct {
	BlocksPerMinute   int           // Max blocks to fetch per minute (default: 5)
	MinInterval       time.Duration // Minimum time between RPC calls
	MaxRetries        int           // Max retries before marking failed
	QuotaWarnPercent  float64       // Pause if quota usage exceeds this (default: 70)
	QuotaCheckEnabled bool          // Whether to check budget before processing
}

// DefaultConfig returns conservative defaults for free tier.
func DefaultConfig() ProcessorConfig {
	return ProcessorConfig{
		BlocksPerMinute:   5,
		MinInterval:       12 * time.Second, // 60/5 = 12s between calls
		MaxRetries:        3,
		QuotaWarnPercent:  70,
		QuotaCheckEnabled: true,
	}
}

// Processor handles background processing of missing blocks.
type Processor struct {
	config      ProcessorConfig
	missingRepo storage.MissingBlockRepository
	fetcher     BlockFetcher
	budget      rpc.BudgetTracker

	mu            sync.RWMutex
	lastProcessed map[string]time.Time
	stats         map[string]*ProcessorStats
}

// ProcessorStats tracks processing statistics.
type ProcessorStats struct {
	PendingCount   int
	ProcessedCount int
	FailedCount    int
	LastProcessed  time.Time
}

// SetBudgetTracker sets the budget tracker for quota checking.
func (p *Processor) SetBudgetTracker(b rpc.BudgetTracker) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.budget = b
}

// ProcessOne fetches ONE missing block, respecting rate limits.
// Returns ErrQuotaExceeded if quota is too high.
// Returns ErrNoMissingBlocks if queue is empty.
func (p *Processor) ProcessOne(ctx context.Context, chainID string) error {
	// Check rate limit
	if !p.canProcess(chainID) {
		delay := p.getDelay(chainID)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Check quota
	if p.config.QuotaCheckEnabled && p.budget != nil {
		usage := p.budget.GetUsage(chainID)
		if usage.UsagePercentage >= p.config.QuotaWarnPercent {
			return ErrQuotaExceeded
		}
	}

	// Get next missing block
	missing, err := p.missingRepo.GetNext(ctx, chainID)
	if err != nil {
		return fmt.Errorf("failed to get next missing block: %w", err)
	}
	if missing == nil {
		return ErrNoMissingBlocks
	}

	// Mark as processing
	if err := p.missingRepo.MarkProcessing(ctx, missing.ID); err != nil {
		return fmt.Errorf("failed to mark processing: %w", err)
	}

	// Process each block in the range
	success := true
	for blockNum := missing.FromBlock; blockNum <= missing.ToBlock; blockNum++ {
		if err := p.fetcher(chainID, blockNum); err != nil {
			success = false
			break
		}
	}

	// Update status
	if success {
		if err := p.missingRepo.MarkCompleted(ctx, missing.ID); err != nil {
			return fmt.Errorf("failed to mark completed: %w", err)
		}
		p.recordProcessed(chainID)
	} else {
		if missing.RetryCount >= p.config.MaxRetries {
			p.missingRepo.MarkFailed(ctx, missing.ID, "max retries exceeded")
			p.recordFailed(chainID)
		}
	}

	return nil
}

// Run starts background processing. Blocks until context is cancelled.
func (p *Processor) Run(ctx context.Context, chainID string) error {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// Initial update
	if count, err := p.missingRepo.Count(ctx, chainID); err == nil {
		metrics.BackfillBlocksQueued.WithLabelValues(chainID).Set(float64(count))
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if count, err := p.missingRepo.Count(ctx, chainID); err == nil {
				metrics.BackfillBlocksQueued.WithLabelValues(chainID).Set(float64(count))
			}
		default:
			err := p.ProcessOne(ctx, chainID)
			if errors.Is(err, ErrNoMissingBlocks) {
				// Queue empty, wait before checking again
				time.Sleep(30 * time.Second)
			} else if errors.Is(err, ErrQuotaExceeded) {
				// Quota too high, wait longer
				time.Sleep(5 * time.Minute)
			} else if err != nil {
				// Other error, brief pause
				time.Sleep(5 * time.Second)
			}
		}
	}
}

// GetStats returns processing statistics for a chain.
func (p *Processor) GetStats(chainID string) ProcessorStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.stats == nil {
		return ProcessorStats{}
	}
	if s, ok := p.stats[chainID]; ok {
		return *s
	}
	return ProcessorStats{}
}

func (p *Processor) canProcess(chainID string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.lastProcessed == nil {
		return true
	}
	last, ok := p.lastProcessed[chainID]
	if !ok {
		return true
	}
	return time.Since(last) >= p.config.MinInterval
}

func (p *Processor) getDelay(chainID string) time.Duration {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.lastProcessed == nil {
		return 0
	}
	last, ok := p.lastProcessed[chainID]
	if !ok {
		return 0
	}
	elapsed := time.Since(last)
	if elapsed >= p.config.MinInterval {
		return 0
	}
	return p.config.MinInterval - elapsed
}

func (p *Processor) recordProcessed(chainID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.lastProcessed == nil {
		p.lastProcessed = make(map[string]time.Time)
	}
	if p.stats == nil {
		p.stats = make(map[string]*ProcessorStats)
	}

	p.lastProcessed[chainID] = time.Now()

	if _, ok := p.stats[chainID]; !ok {
		p.stats[chainID] = &ProcessorStats{}
	}
	p.stats[chainID].ProcessedCount++
	p.stats[chainID].LastProcessed = time.Now()

	// Update metrics
	metrics.BackfillBlocksProcessed.WithLabelValues(chainID).Inc()
}

func (p *Processor) recordFailed(chainID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stats == nil {
		p.stats = make(map[string]*ProcessorStats)
	}
	if _, ok := p.stats[chainID]; !ok {
		p.stats[chainID] = &ProcessorStats{}
	}
	p.stats[chainID].FailedCount++
}
