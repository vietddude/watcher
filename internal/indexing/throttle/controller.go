package throttle

import (
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
)

// AdaptiveController computes optimal scan intervals and batch sizes
// based on current lag and RPC performance metrics.
type AdaptiveController struct {
	chainID          domain.ChainID
	baseScanInterval time.Duration
	config           AdaptiveConfig

	// Current state (for metrics)
	currentInterval  time.Duration
	currentBatchSize int
}

// NewAdaptiveController creates a new adaptive controller.
func NewAdaptiveController(
	chainID domain.ChainID,
	baseScanInterval time.Duration,
	config AdaptiveConfig,
) *AdaptiveController {
	return &AdaptiveController{
		chainID:          chainID,
		baseScanInterval: baseScanInterval,
		config:           config,
		currentInterval:  baseScanInterval,
		currentBatchSize: 1,
	}
}

// ComputeInterval calculates the optimal scan interval based on lag.
//
// Algorithm:
//   - lag ≤ 0: Use base interval (at chain head, save API calls)
//   - lag < normal: Use base interval × 0.5 (slightly behind)
//   - lag < burst: Use min interval × 2 (catching up)
//   - lag ≥ burst: Use min interval (maximum catchup speed)
func (c *AdaptiveController) ComputeInterval(lag int64) time.Duration {
	if !c.config.Enabled {
		return c.baseScanInterval
	}

	var interval time.Duration

	switch {
	case lag <= 0:
		// At chain head - use base interval to save API calls
		interval = c.baseScanInterval

	case lag < c.config.LagNormalThreshold:
		// Slightly behind - speed up a bit
		interval = c.baseScanInterval / 2

	case lag < c.config.LagBurstThreshold:
		// Catching up - use faster interval
		interval = c.config.MinScanInterval * 2

	default:
		// Far behind - maximum speed
		interval = c.config.MinScanInterval
	}

	// Enforce bounds
	if interval < c.config.MinScanInterval {
		interval = c.config.MinScanInterval
	}
	if interval > c.config.MaxScanInterval {
		interval = c.config.MaxScanInterval
	}

	c.currentInterval = interval
	return interval
}

// ComputeBatchSize calculates the optimal batch size based on lag and RPC latency.
//
// Algorithm:
//   - High latency (>2s): Use min batch size (RPC is slow)
//   - lag ≤ 0: Batch size 1 (at head, just check next block)
//   - lag < 10: Batch size 3
//   - lag < 100: Batch size 10
//   - lag ≥ 100: Max batch size (maximum catchup)
func (c *AdaptiveController) ComputeBatchSize(lag int64, avgLatency time.Duration) int {
	if !c.config.Enabled || !c.config.BatchEnabled {
		return 1
	}

	// If RPC is slow, use conservative batch size
	if avgLatency > c.config.HighLatencyThreshold {
		c.currentBatchSize = c.config.MinBatchSize
		return c.currentBatchSize
	}

	var batchSize int

	switch {
	case lag <= 0:
		// At chain head - just check next block
		batchSize = 1

	case lag < 10:
		// Slightly behind - small batch
		batchSize = min(3, c.config.MaxBatchSize)

	case lag < 100:
		// Catching up - medium batch
		batchSize = min(10, c.config.MaxBatchSize)

	default:
		// Far behind - maximum batch for fastest catchup
		batchSize = c.config.MaxBatchSize
	}

	// Enforce bounds
	if batchSize < c.config.MinBatchSize {
		batchSize = c.config.MinBatchSize
	}
	if batchSize > c.config.MaxBatchSize {
		batchSize = c.config.MaxBatchSize
	}

	c.currentBatchSize = batchSize
	return batchSize
}

// GetCurrentInterval returns the last computed interval (for metrics).
func (c *AdaptiveController) GetCurrentInterval() time.Duration {
	return c.currentInterval
}

// GetCurrentBatchSize returns the last computed batch size (for metrics).
func (c *AdaptiveController) GetCurrentBatchSize() int {
	return c.currentBatchSize
}

// min returns the minimum of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
