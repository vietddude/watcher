package health

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/vietddude/watcher/internal/core/cursor"
	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/indexing/metrics"
	"github.com/vietddude/watcher/internal/infra/rpc"
	"github.com/vietddude/watcher/internal/infra/storage"
)

// BlockHeightFetcher fetches the latest block height for a chain.
type BlockHeightFetcher interface {
	GetLatestHeight(ctx context.Context, chainID string) (uint64, error)
}

// Monitor aggregates health status from various system components.
type Monitor struct {
	chains        []string
	chainNames    map[string]string // ID -> Name map
	providers     map[string][]string
	cursorMgr     cursor.Manager
	missingRepo   storage.MissingBlockRepository
	failedRepo    storage.FailedBlockRepository
	budgetTracker rpc.BudgetTracker
	heightFetcher BlockHeightFetcher
	lastCheck     time.Time
	lastReport    map[string]ChainHealth
	scanIntervals map[string]time.Duration
	mu            sync.RWMutex
}

// NewMonitor creates a new health monitor.
func NewMonitor(
	chainNames map[string]string,
	providers map[string][]string,
	cursorMgr cursor.Manager,
	missingRepo storage.MissingBlockRepository,
	failedRepo storage.FailedBlockRepository,
	budgetTracker rpc.BudgetTracker,
	heightFetcher BlockHeightFetcher,
	scanIntervals map[string]time.Duration,
) *Monitor {
	chains := make([]string, 0, len(chainNames))
	for id := range chainNames {
		chains = append(chains, id)
	}

	return &Monitor{
		chains:        chains,
		chainNames:    chainNames,
		providers:     providers,
		cursorMgr:     cursorMgr,
		missingRepo:   missingRepo,
		failedRepo:    failedRepo,
		budgetTracker: budgetTracker,
		heightFetcher: heightFetcher,
		scanIntervals: scanIntervals,
		lastReport:    make(map[string]ChainHealth),
	}
}

// CheckHealth performs a health check for all chains.
func (m *Monitor) CheckHealth(ctx context.Context) map[string]ChainHealth {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Rate limit checks (e.g. max once per 10s) to avoid spamming RPC
	if time.Since(m.lastCheck) < 10*time.Second && len(m.lastReport) > 0 {
		return m.lastReport
	}

	report := make(map[string]ChainHealth)

	for _, chainID := range m.chains {

		health := ChainHealth{
			ChainID: chainID,
			Status:  StatusHealthy,
		}

		// 1. Get Block Lag
		latestHeight, err := m.heightFetcher.GetLatestHeight(ctx, chainID)
		if err == nil {
			lag, err := m.cursorMgr.GetLag(ctx, chainID, latestHeight)
			if err == nil && lag > 0 {
				health.BlockLag = uint64(lag)

				// Performance Suggestion
				if lag > 10 && lag <= 100 {
					if currentInterval, ok := m.scanIntervals[chainID]; ok {
						suggested := currentInterval / 2
						chainName := m.chainNames[chainID]
						if chainName == "" {
							chainName = chainID
						}
						slog.Warn("Performance Warning: Indexer is lagging behind chain",
							"chain", chainName,
							"lag", lag,
							"current_interval", currentInterval,
							"suggested_interval", suggested,
							"msg", "Consider reducing scan_interval in config.yaml to catch up",
						)
					}
				}

				// [NEW] Gap Detection and Auto-Jump
				// If we are significantly behind (>100 blocks), we jump to head
				// and queue the missing range for backfill.
				if lag > 100 {
					currentCursor, _ := m.cursorMgr.Get(ctx, chainID)
					// Verify we have a cursor (we should if lag is calculated)
					if currentCursor != nil {
						// Define the gap: (current + 1) -> (latest - 1)
						// The pipeline will pick up 'latest' after the jump.
						gapFrom := currentCursor.CurrentBlock + 1
						gapTo := latestHeight - 1

						if gapTo >= gapFrom {
							slog.Warn("Detected significant lag, triggering auto-backfill",
								"chain", chainID,
								"lag", lag,
								"gapFrom", gapFrom,
								"gapTo", gapTo,
							)

							// 1. Create MissingBlock task
							// Priority 10 (High) because it's recent history
							missing := &domain.MissingBlock{
								ID:        uuid.New().String(),
								ChainID:   chainID,
								FromBlock: gapFrom,
								ToBlock:   gapTo,
								Status:    domain.MissingBlockStatusPending,
								Priority:  10,
								CreatedAt: uint64(time.Now().Unix()),
							}

							if err := m.missingRepo.Add(ctx, missing); err != nil {
								slog.Error("Failed to queue backfill task", "chain", chainID, "error", err)
							} else {
								slog.Info("Queued backfill task", "chain", chainID, "range", fmt.Sprintf("%d-%d", gapFrom, gapTo))

								// 2. Jump Cursor to Latest Height
								// This allows the real-time indexer to resume from the tip immediately.
								// We jump to (latest - 1) essentially, so next text 'latest' is processed?
								// Use latestHeight - 1 so the pipeline picks up 'latestHeight' as next block.
								// Or if we jump to 'latestHeight', pipeline waits for 'latestHeight+1'.
								// Let's jump to latestHeight to start fresh from NEXT block, or reuse latest?
								// Pipeline does: target = cursor + 1.
								// If we want to process latestHeight, cursor should be latestHeight - 1.
								jumpTarget := latestHeight - 1
								if err := m.cursorMgr.Jump(ctx, chainID, jumpTarget); err != nil {
									slog.Error("Failed to jump cursor", "chain", chainID, "error", err)
								} else {
									slog.Info("Jumped cursor to tip", "chain", chainID, "target", jumpTarget)
									// Reset lag for report
									health.BlockLag = 0
								}
							}
						}
					}
				}
			}
		}
		count, err := m.missingRepo.Count(ctx, chainID)
		if err == nil {
			health.MissingBlocks = count
			if name, ok := m.chainNames[chainID]; ok {
				metrics.BackfillBlocksQueued.WithLabelValues(name).Set(float64(count))
			} else {
				metrics.BackfillBlocksQueued.WithLabelValues(chainID).Set(float64(count))
			}
		}

		// 3. Failed Blocks
		failedCount, err := m.failedRepo.Count(ctx, chainID)
		if err == nil {
			health.FailedBlocks = failedCount
		}

		// 4. RPC Quota Usage

		// 4. RPC Quota Usage
		if providers, ok := m.providers[chainID]; ok {
			chainName := m.chainNames[chainID]
			if chainName == "" {
				chainName = chainID
			}
			for _, provider := range providers {
				pUsage := m.budgetTracker.GetProviderUsage(chainID, provider)
				metrics.RPCProviderQuotaUsage.WithLabelValues(chainName, provider).Set(pUsage.UsagePercentage / 100.0)
				metrics.RPCQuotaRemaining.WithLabelValues(chainName, provider).Set(float64(pUsage.RemainingCalls))
			}
		}

		// Evaluate Status
		if health.BlockLag > 100 || health.MissingBlocks > 10 || health.FailedBlocks > 50 {
			health.Status = StatusCritical
		} else if health.BlockLag > 10 || health.MissingBlocks > 0 || health.FailedBlocks > 0 {
			health.Status = StatusDegraded
		}

		report[chainID] = health
	}

	m.lastCheck = time.Now()
	m.lastReport = report
	return report
}

// Start begins the background health check loop to ensure metrics are updated periodically.
func (m *Monitor) Start(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Run once immediately
	m.CheckHealth(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.CheckHealth(ctx)
		}
	}
}
