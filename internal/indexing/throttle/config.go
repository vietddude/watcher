package throttle

import "time"

// AdaptiveConfig holds configuration for adaptive throttling behavior.
type AdaptiveConfig struct {
	// Enabled controls whether adaptive throttling is active
	Enabled bool

	// Interval bounds
	MinScanInterval time.Duration // Fastest polling rate (default: 500ms)
	MaxScanInterval time.Duration // Slowest polling rate (default: 60s)

	// Head caching
	HeadCacheTTL time.Duration // How long to cache GetLatestBlock result (default: 3s)

	// Lag thresholds for interval adjustment
	LagNormalThreshold int64 // Below this = normal interval (default: 5)
	LagBurstThreshold  int64 // Above this = max speed (default: 50)

	// Batch settings (for chains that support BatchAdapter)
	BatchEnabled bool // Enable batch block fetching
	MinBatchSize int  // Minimum blocks per batch (default: 1)
	MaxBatchSize int  // Maximum blocks per batch (default: 20)

	// Latency thresholds for batch adjustment
	LowLatencyThreshold  time.Duration // Below this = can increase batch (default: 500ms)
	HighLatencyThreshold time.Duration // Above this = decrease batch (default: 2s)
}

// DefaultConfig returns sensible defaults for adaptive throttling.
func DefaultConfig() AdaptiveConfig {
	return AdaptiveConfig{
		Enabled:              true,
		MinScanInterval:      500 * time.Millisecond,
		MaxScanInterval:      60 * time.Second,
		HeadCacheTTL:         3 * time.Second,
		LagNormalThreshold:   5,
		LagBurstThreshold:    50,
		BatchEnabled:         true,
		MinBatchSize:         1,
		MaxBatchSize:         20,
		LowLatencyThreshold:  500 * time.Millisecond,
		HighLatencyThreshold: 2 * time.Second,
	}
}
