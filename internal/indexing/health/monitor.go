package health

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/vietddude/watcher/internal/core/cursor"
	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/indexing/metrics"
	"github.com/vietddude/watcher/internal/infra/rpc"
	"github.com/vietddude/watcher/internal/infra/storage"
)

// BlockHeightFetcher fetches the latest block height for a chain.
type BlockHeightFetcher interface {
	GetLatestHeight(ctx context.Context, chainID domain.ChainID) (uint64, error)
}

// Monitor aggregates health status from various system components.
type Monitor struct {
	chains        []domain.ChainID
	providers     map[domain.ChainID][]string
	cursorMgr     cursor.Manager
	missingRepo   storage.MissingBlockRepository
	failedRepo    storage.FailedBlockRepository
	budgetTracker rpc.BudgetTracker
	heightFetcher BlockHeightFetcher
	routers       map[domain.ChainID]rpc.Router
	lastCheck     time.Time
	lastReport    map[domain.ChainID]ChainHealth
	scanIntervals map[domain.ChainID]time.Duration
	mu            sync.RWMutex
	lastLag       map[domain.ChainID]uint64
	highLagCount  map[domain.ChainID]int
}

// NewMonitor creates a new health monitor.
func NewMonitor(
	providers map[domain.ChainID][]string,
	cursorMgr cursor.Manager,
	missingRepo storage.MissingBlockRepository,
	failedRepo storage.FailedBlockRepository,
	budgetTracker rpc.BudgetTracker,
	heightFetcher BlockHeightFetcher,
	scanIntervals map[domain.ChainID]time.Duration,
	routers map[domain.ChainID]rpc.Router,
) *Monitor {
	var chains []domain.ChainID
	for id := range providers {
		chains = append(chains, id)
	}

	return &Monitor{
		chains:        chains,
		providers:     providers,
		cursorMgr:     cursorMgr,
		missingRepo:   missingRepo,
		failedRepo:    failedRepo,
		budgetTracker: budgetTracker,
		heightFetcher: heightFetcher,
		scanIntervals: scanIntervals,
		routers:       routers,
		lastReport:    make(map[domain.ChainID]ChainHealth),
		lastLag:       make(map[domain.ChainID]uint64),
		highLagCount:  make(map[domain.ChainID]int),
	}
}

// CheckHealth performs a health check for all chains.
func (m *Monitor) CheckHealth(ctx context.Context) map[domain.ChainID]ChainHealth {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Rate limit checks (e.g. max once per 10s) to avoid spamming RPC
	if time.Since(m.lastCheck) < 10*time.Second && len(m.lastReport) > 0 {
		return m.lastReport
	}

	report := make(map[domain.ChainID]ChainHealth)

	for _, chainID := range m.chains {

		health := ChainHealth{
			ChainID: chainID,
			Status:  StatusHealthy,
		}

		// 1. Get Block Lag
		latestHeight, err := m.heightFetcher.GetLatestHeight(ctx, chainID)

		// Emit metrics ideally even if there is an error (using last known?), but here we only emit on success
		if err == nil {
			lag, err := m.cursorMgr.GetLag(ctx, chainID, latestHeight)
			if err == nil {
				health.BlockLag = uint64(lag)

				// Get current cursor for metrics
				cursor, _ := m.cursorMgr.Get(ctx, chainID)

				// Update Prometheus Metrics for Chain Status
				chainName, _ := domain.ChainNameFromID(chainID)
				metrics.ChainLatestBlock.WithLabelValues(chainName).Set(float64(latestHeight))
				metrics.ChainLag.WithLabelValues(chainName).Set(float64(lag))
				if cursor != nil {
					metrics.IndexerLatestBlock.WithLabelValues(chainName).
						Set(float64(cursor.BlockNumber))
				}

				// [IMPROVED] Intelligent Gap Detection and Auto-Jump
				// 1. Determine Dynamic Threshold
				threshold := uint64(1000) // Default: 1000 blocks
				if chainID == domain.SuiTestnet {
					threshold = 5000 // Sui moves fast, allow more buffer
				}

				// 2. Trend Analysis
				isGrowing := true
				if prevLag, ok := m.lastLag[chainID]; ok {
					if uint64(lag) < prevLag {
						isGrowing = false
					}
				}
				m.lastLag[chainID] = uint64(lag)

				// 3. Evaluation
				shouldTrigger := false
				if uint64(lag) > threshold {
					m.highLagCount[chainID]++

					// Conditions to trigger:
					// A. Lag is growing AND above threshold
					// B. Lag is persistently high (> 3 checks) even if stable
					if isGrowing {
						shouldTrigger = true
					} else if m.highLagCount[chainID] >= 3 {
						shouldTrigger = true
						slog.Info("Lag is persistently high (stalled), triggering backfill", "chain", chainID, "lag", lag)
					}
				} else {
					m.highLagCount[chainID] = 0
				}

				if shouldTrigger {
					currentCursor, _ := m.cursorMgr.Get(ctx, chainID)
					// Verify we have a cursor (we should if lag is calculated)
					if currentCursor != nil {
						// Define the gap: (current + 1) -> (latest - 1)
						gapFrom := currentCursor.BlockNumber + 1
						gapTo := latestHeight - 1

						if gapTo >= gapFrom {
							// Pipeline will handle the jump automatically
							// We just clear highLagCount here to avoid repeated detections
							m.highLagCount[chainID] = 0
						}
					}
				}
			}
		}
		count, err := m.missingRepo.Count(ctx, chainID)
		if err == nil {
			health.MissingBlocks = count
			chainName, _ := domain.ChainNameFromID(chainID)
			metrics.BackfillBlocksQueued.WithLabelValues(chainName).Set(float64(count))
		}

		// 3. Failed Blocks
		failedCount, err := m.failedRepo.Count(ctx, chainID)
		if err == nil {
			health.FailedBlocks = failedCount
		}

		// 4. RPC Quota Usage
		if providers, ok := m.providers[chainID]; ok {
			chainName, _ := domain.ChainNameFromID(chainID)
			for _, provider := range providers {
				pUsage := m.budgetTracker.GetProviderUsage(provider)
				metrics.RPCProviderQuotaUsage.WithLabelValues(chainName, provider).
					Set(pUsage.UsagePercentage / 100.0)
				metrics.RPCQuotaRemaining.WithLabelValues(chainName, provider).
					Set(float64(pUsage.RemainingCalls))
			}
		}

		// 5. Chain Availability (Check if ANY provider is functional)
		isAvailable := 0.0
		if router, ok := m.routers[chainID]; ok {
			_, err := router.GetProvider(chainID)
			if err == nil {
				isAvailable = 1.0
			} else {
				slog.Error("CRITICAL: No functional providers available for chain", "chain", chainID, "error", err)
			}
		}
		chainName, _ := domain.ChainNameFromID(chainID)
		metrics.ChainAvailable.WithLabelValues(chainName).Set(isAvailable)

		// Evaluate Status
		if isAvailable == 0 {
			health.Status = StatusCritical
		} else if health.BlockLag > 100 || health.MissingBlocks > 10 || health.FailedBlocks > 50 {
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
