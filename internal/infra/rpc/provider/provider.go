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

	"google.golang.org/grpc"
)

// Operation represents an RPC operation to execute.
// It abstracts the transport layer for unified routing/budget logic.
type Operation struct {
	// Name identifies the operation (e.g., "eth_blockNumber", "GetBlock")
	Name string

	// Cost is the quota cost for this operation (default 1)
	Cost int

	// Params for HTTP JSON-RPC calls.
	// NOTE: For HTTP providers, if Invoke is NOT set, these Params are used with Name.
	// For JSON-RPC, this should be []any. For REST, it can be any valid JSON-serializable type.
	Params any

	// IsREST indicates if this is a REST API call instead of JSON-RPC.
	IsREST bool

	// RESTMethod specifies the HTTP method for REST calls (e.g., "GET", "POST").
	// Only used if IsREST is true.
	RESTMethod string

	// JSONRPCVersion specifies the JSON-RPC version (e.g., "1.0", "2.0").
	// If empty, defaults to "2.0".
	JSONRPCVersion string

	// Invoke is the actual operation to execute.
	// NOTE: For gRPC providers, this is REQUIRED and wraps the generated client call.
	// NOTE: For HTTP providers, if set, this takes precedence over Params.
	Invoke func(ctx context.Context) (any, error)

	// GRPCHandler is a function that takes a gRPC connection and executes the operation.
	// This enables load-balanced gRPC calls where the provider injects the connection.
	GRPCHandler func(ctx context.Context, conn grpc.ClientConnInterface) (any, error)
}

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

	// HasCapacity checks if the provider has capacity for the given cost
	HasCapacity(cost int) bool

	// Execute performs the operation with monitoring and error handling
	Execute(ctx context.Context, op Operation) (any, error)

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
