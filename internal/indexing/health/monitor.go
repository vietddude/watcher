package health

import (
	"context"
	"sync"
	"time"

	"github.com/vietddude/watcher/internal/core/cursor"
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
	cursorMgr     cursor.Manager
	missingRepo   storage.MissingBlockRepository
	failedRepo    storage.FailedBlockRepository
	budgetTracker rpc.BudgetTracker
	heightFetcher BlockHeightFetcher
	lastCheck     time.Time
	lastReport    map[string]ChainHealth
	mu            sync.RWMutex
}

// NewMonitor creates a new health monitor.
func NewMonitor(
	chainNames map[string]string,
	cursorMgr cursor.Manager,
	missingRepo storage.MissingBlockRepository,
	failedRepo storage.FailedBlockRepository,
	budgetTracker rpc.BudgetTracker,
	heightFetcher BlockHeightFetcher,
) *Monitor {
	chains := make([]string, 0, len(chainNames))
	for id := range chainNames {
		chains = append(chains, id)
	}

	return &Monitor{
		chains:        chains,
		chainNames:    chainNames,
		cursorMgr:     cursorMgr,
		missingRepo:   missingRepo,
		failedRepo:    failedRepo,
		budgetTracker: budgetTracker,
		heightFetcher: heightFetcher,
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
		chainName := m.chainNames[chainID]
		health := ChainHealth{
			ChainID: chainID,
			Status:  StatusHealthy,
		}

		// 1. Get Block Lag

		// 2. Missing Blocks
		count, err := m.missingRepo.Count(ctx, chainID)
		if err == nil {
			health.MissingBlocks = count
			metrics.MissingBlocksCount.WithLabelValues(chainName).Set(float64(count))
		}

		// 3. Failed Blocks
		failedCount, err := m.failedRepo.Count(ctx, chainID)
		if err == nil {
			health.FailedBlocks = failedCount
			metrics.FailedBlocksCount.WithLabelValues(chainName).Set(float64(failedCount))
		}

		// 4. RPC Quota Usage
		quotaPercent := m.budgetTracker.GetUsagePercent()
		metrics.RPCQuotaUsedPercent.WithLabelValues(chainName).Set(quotaPercent)

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
