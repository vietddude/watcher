package health

import (
	"context"
	"sync"
	"time"

	"github.com/vietddude/watcher/internal/core/cursor"
	"github.com/vietddude/watcher/internal/infra/rpc/budget"
	"github.com/vietddude/watcher/internal/infra/storage"
)

// BlockHeightFetcher fetches the latest block height for a chain.
type BlockHeightFetcher interface {
	GetLatestHeight(ctx context.Context, chainID string) (uint64, error)
}

// Monitor aggregates health status from various system components.
type Monitor struct {
	chains        []string
	cursorMgr     cursor.Manager
	missingRepo   storage.MissingBlockRepository
	failedRepo    storage.FailedBlockRepository
	budgetTracker budget.BudgetTracker
	heightFetcher BlockHeightFetcher
	lastCheck     time.Time
	lastReport    map[string]ChainHealth
	mu            sync.RWMutex
}

// NewMonitor creates a new health monitor.
func NewMonitor(
	chains []string,
	cursorMgr cursor.Manager,
	missingRepo storage.MissingBlockRepository,
	failedRepo storage.FailedBlockRepository,
	budgetTracker budget.BudgetTracker,
	heightFetcher BlockHeightFetcher,
) *Monitor {
	return &Monitor{
		chains:        chains,
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
		health := ChainHealth{
			ChainID: chainID,
			Status:  StatusHealthy,
		}

		// 1. Get Block Lag
		latest, err := m.heightFetcher.GetLatestHeight(ctx, chainID)
		if err != nil {
			// If we can't get height, that's degradation
			health.Status = StatusDegraded
		} else {
			lag, _ := m.cursorMgr.GetLag(ctx, chainID, latest)
			if lag < 0 {
				lag = 0
			}
			health.BlockLag = uint64(lag)
		}

		// 2. Missing Blocks
		count, err := m.missingRepo.Count(ctx, chainID)
		if err == nil {
			health.MissingBlocks = count
		}

		// 3. Failed Blocks
		failedCount, err := m.failedRepo.Count(ctx, chainID)
		if err == nil {
			health.FailedBlocks = failedCount
		}

		// 4. RPC Error Rate (Approximation via budget usage for now, or placeholder)
		// Since we don't have direct error rate metrics exposed from BudgetTracker yet,
		// we'll stick to lag and queue sizes for status determination.
		// Detailed status logic from docs:

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
