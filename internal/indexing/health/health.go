// Package health provides system health monitoring and status reporting.
package health

// SystemStatus represents the overall health state of the system or a component.
type SystemStatus string

const (
	StatusHealthy  SystemStatus = "healthy"
	StatusDegraded SystemStatus = "degraded"
	StatusCritical SystemStatus = "critical"
)

// ChainHealth contains health metrics for a specific blockchain chain.
type ChainHealth struct {
	ChainID       string       `json:"chain_id"`
	Status        SystemStatus `json:"status"`
	BlockLag      uint64       `json:"block_lag"`
	MissingBlocks int          `json:"missing_blocks"`
	FailedBlocks  int          `json:"failed_blocks"`
	RPCErrorRate  float64      `json:"rpc_error_rate"`
}

// HealthReport contains the full system health report.
type HealthReport struct {
	SystemStatus SystemStatus           `json:"system_status"`
	Chains       map[string]ChainHealth `json:"chains"`
}
