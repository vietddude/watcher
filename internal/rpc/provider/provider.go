// Package provider implements RPC provider interfaces and HTTP-based providers.
//
// This package contains:
//   - Provider interface: core abstraction for RPC endpoints
//   - HTTPProvider: JSON-RPC over HTTP implementation
//   - ProviderMonitor: health and rate tracking
//   - ProviderPool: connection pooling with health checks
package provider

import (
	"context"
	"time"
)

// Provider defines the core RPC provider interface.
// This is the main abstraction for connecting to different RPC endpoints.
type Provider interface {
	// Call makes a single RPC request
	Call(ctx context.Context, method string, params []any) (any, error)

	// BatchCall makes multiple RPC calls in one request (if supported)
	BatchCall(ctx context.Context, requests []BatchRequest) ([]BatchResponse, error)

	// GetName returns provider identifier (e.g., "alchemy", "infura")
	GetName() string

	// GetHealth returns current health metrics
	GetHealth() HealthStatus

	// Close cleans up resources
	Close() error
}

// BatchRequest represents a single request in a batch call.
type BatchRequest struct {
	Method string
	Params []any
}

// BatchResponse represents a single response from a batch call.
type BatchResponse struct {
	Result any
	Error  error
}

// HealthStatus represents the health state of a provider.
type HealthStatus struct {
	Available     bool
	Latency       time.Duration
	ErrorRate     float64
	LastSuccessAt time.Time
	LastFailureAt time.Time
	MonitorStats  *MonitorStats `json:"monitor_stats,omitempty"`
}
