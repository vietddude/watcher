// Package provider implements RPC provider interfaces.
//
// This package contains:
//   - Provider interface: core abstraction for RPC endpoints
//   - HTTPProvider: JSON-RPC over HTTP implementation
//   - GRPCProvider: gRPC implementation
//   - ProviderMonitor: health and rate tracking
//   - ProviderPool: connection pooling with health checks
package provider

import (
	"context"
	"time"
)

// Provider defines the core interface for any RPC provider (HTTP or gRPC).
// It serves as the base abstraction for health checking, metrics, and lifecycle management.
type Provider interface {
	// GetName returns provider identifier (e.g., "alchemy", "infura")
	GetName() string

	// GetHealth returns current health metrics
	GetHealth() HealthStatus

	// IsAvailable checks if the provider is healthy enough to use
	IsAvailable() bool

	// HasQuotaRemaining checks if the provider has not exceeded its rate limits
	HasQuotaRemaining() bool

	// Close cleans up resources
	Close() error
}

// RPCProvider extends Provider with methods for making JSON-RPC calls.
// This is typically implemented by HTTP-based providers (JSON-RPC).
type RPCProvider interface {
	Provider

	// Call makes a single RPC request
	Call(ctx context.Context, method string, params []any) (any, error)

	// BatchCall makes multiple RPC calls in one request
	BatchCall(ctx context.Context, requests []BatchRequest) ([]BatchResponse, error)
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
