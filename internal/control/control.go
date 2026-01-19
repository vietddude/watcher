package control

import (
	"time"
)

// HealthMonitor monitors system health
type HealthMonitor interface {
	// GetHealth returns overall health status
	GetHealth() HealthStatus

	// CheckChainHealth checks health of a specific chain
	CheckChainHealth(chainID string) ChainHealth
}

type HealthStatus struct {
	Healthy   bool
	Chains    map[string]ChainHealth
	UpdatedAt time.Time
}

type ChainHealth struct {
	State         string
	BlockLag      int64
	MissingBlocks int
	FailedBlocks  int
	RPCErrorRate  float64
	Status        string // "healthy", "degraded", "critical"
}

// MetricsCollector collects and exposes metrics
type MetricsCollector interface {
	// RecordBlockProcessed records a processed block
	RecordBlockProcessed(chainID string, blockNumber uint64, duration time.Duration)

	// RecordRPCCall records an RPC call
	RecordRPCCall(
		chainID string,
		provider string,
		method string,
		success bool,
		duration time.Duration,
	)

	// RecordEventEmitted records an emitted event
	RecordEventEmitted(chainID string, eventType string, success bool)

	// RecordReorg records a reorg detection
	RecordReorg(chainID string, depth uint64)

	// GetMetrics returns current metrics snapshot
	GetMetrics() Metrics
}

type Metrics struct {
	BlocksProcessed     int64
	TransactionsIndexed int64
	EventsEmitted       int64
	RPCCalls            int64
	RPCErrors           int64
	ReorgsDetected      int64
}

// Throttler manages adaptive throttling
type Throttler interface {
	// ShouldThrottle checks if operations should be throttled
	ShouldThrottle(chainID string) bool

	// GetThrottleDelay returns the delay to apply
	GetThrottleDelay(chainID string) time.Duration

	// UpdateQuotaUsage updates quota usage information
	UpdateQuotaUsage(chainID string, percentage float64)
}
