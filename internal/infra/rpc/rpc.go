// Package rpc provides a resilient RPC client for blockchain networks.
//
// This package offers robust RPC connectivity with:
//   - Multiple provider support (Alchemy, Infura, etc.)
//   - Automatic failover and rotation
//   - Quota/budget management
//   - Predictive throttling
//   - Health monitoring
//
// # Quick Start
//
//	import "github.com/vietddude/watcher/internal/infra/rpc"
//
//	// Setup
//	budget := rpc.NewBudgetTracker(100000, map[string]float64{"ethereum": 1.0})
//	router := rpc.NewRouter(budget)
//	router.AddProvider("ethereum", rpc.NewHTTPProvider("alchemy", alchemyURL, 30*time.Second))
//	router.AddProvider("ethereum", rpc.NewHTTPProvider("infura", infuraURL, 30*time.Second))
//
//	// Create client
//	client := rpc.NewClient("ethereum", router, budget)
//
//	// Make calls
//	result, err := client.Call(ctx, "eth_blockNumber", nil)
//
// # Advanced Usage (with Coordinator)
//
// For predictive rotation and unified budget-router coordination:
//
//	coordinator := rpc.NewCoordinator(router, budget)
//	coordinator.SetRotationCallback(func(chain, from, to, reason string) {
//	    log.Printf("Rotated from %s to %s: %s", from, to, reason)
//	})
//	client := rpc.NewClientWithCoordinator("ethereum", coordinator)
//
// # Package Structure
//
// The package is organized into sub-packages for maintainability:
//
//   - provider/ - Provider implementations (HTTPProvider, monitoring, pooling)
//   - routing/  - Provider selection, rotation strategies, retry logic
//   - budget/   - Quota tracking, prediction, coordination
//
// Most types are re-exported at the root level for convenience.
package rpc

import (
	"context"
	"time"

	"github.com/vietddude/watcher/internal/infra/rpc/budget"
	"github.com/vietddude/watcher/internal/infra/rpc/provider"
	"github.com/vietddude/watcher/internal/infra/rpc/routing"
)

// =============================================================================
// Re-exported types from provider package
// =============================================================================

// Provider is the core interface for RPC endpoints.
type Provider = provider.Provider

// RPCProvider is the interface for providers that support JSON-RPC calls.
type RPCProvider = provider.RPCProvider

// HTTPProvider implements Provider for JSON-RPC over HTTP.
type HTTPProvider = provider.HTTPProvider

// GRPCProvider implements Provider for gRPC.
type GRPCProvider = provider.GRPCProvider

// ProviderMonitor tracks provider health and rate limiting.
type ProviderMonitor = provider.ProviderMonitor

// ProviderPool manages a pool of provider connections.
type ProviderPool = provider.ProviderPool

// ProviderStatus represents the health state of a provider.
type ProviderStatus = provider.ProviderStatus

// MonitorStats holds monitoring statistics for a provider.
type MonitorStats = provider.MonitorStats

// HealthStatus represents the health state of a provider.
type HealthStatus = provider.HealthStatus

// BatchRequest represents a single request in a batch call.
type BatchRequest = provider.BatchRequest

// BatchResponse represents a single response from a batch call.
type BatchResponse = provider.BatchResponse

// Operation represents an RPC operation to execute (transport-agnostic).
type Operation = provider.Operation

// Provider status constants
const (
	StatusHealthy   = provider.StatusHealthy
	StatusDegraded  = provider.StatusDegraded
	StatusThrottled = provider.StatusThrottled
	StatusBlocked   = provider.StatusBlocked
)

// NewHTTPProvider creates a new HTTP-based RPC provider.
func NewHTTPProvider(name, endpoint string, timeout time.Duration) *HTTPProvider {
	return provider.NewHTTPProvider(name, endpoint, timeout)
}

// NewGRPCProvider creates a new gRPC provider.
func NewGRPCProvider(ctx context.Context, name, endpoint string) (*GRPCProvider, error) {
	return provider.NewGRPCProvider(ctx, name, endpoint)
}

// NewProviderPool creates a new provider pool.
func NewProviderPool(maxSize int, healthCheckInterval time.Duration) *ProviderPool {
	return provider.NewProviderPool(maxSize, healthCheckInterval)
}

// =============================================================================
// Re-exported types from routing package
// =============================================================================

// Router handles provider selection and health tracking.
type Router = routing.Router

// DefaultRouter implements smart provider selection with circuit breaker.
type DefaultRouter = routing.DefaultRouter

// ProviderRotator handles provider rotation with multiple strategies.
type ProviderRotator = routing.ProviderRotator

// RotationStrategy defines how providers are rotated.
type RotationStrategy = routing.RotationStrategy

// RetryConfig defines retry behavior.
type RetryConfig = routing.RetryConfig

// Rotation strategy constants
const (
	RotationRoundRobin = routing.RotationRoundRobin
	RotationWeighted   = routing.RotationWeighted
	RotationAdaptive   = routing.RotationAdaptive
	RotationProactive  = routing.RotationProactive
)

// DefaultRetryConfig provides sensible retry defaults.
var DefaultRetryConfig = routing.DefaultRetryConfig

// NewRouter creates a new router with round-robin rotation.
func NewRouter(b budget.BudgetTracker) *DefaultRouter {
	return routing.NewRouter(b)
}

// NewRouterWithStrategy creates a router with a specific rotation strategy.
func NewRouterWithStrategy(b budget.BudgetTracker, strategy RotationStrategy) *DefaultRouter {
	return routing.NewRouterWithStrategy(b, strategy)
}

// CallWithRetry executes an RPC call with exponential backoff.
var CallWithRetry = routing.CallWithRetry

// CallWithRetryAndFailover tries multiple providers with retry.
var CallWithRetryAndFailover = routing.CallWithRetryAndFailover

// =============================================================================
// Re-exported types from budget package
// =============================================================================

// BudgetTracker manages RPC quota and rate limiting.
type BudgetTracker = budget.BudgetTracker

// DefaultBudgetTracker implements BudgetTracker with per-chain tracking.
type DefaultBudgetTracker = budget.DefaultBudgetTracker

// UsageStats holds quota usage statistics.
type UsageStats = budget.UsageStats

// Coordinator unifies Budget and Router for coordinated decisions.
type Coordinator = budget.Coordinator

// CoordinatorConfig holds configuration for the Coordinator.
type CoordinatorConfig = budget.CoordinatorConfig

// BudgetConfig holds budget configuration.
type BudgetConfig = budget.Config

// CoordinatedProvider wraps a Coordinator to implement the Provider interface.
type CoordinatedProvider = budget.CoordinatedProvider

// NewBudgetTracker creates a new budget tracker.
func NewBudgetTracker(dailyLimit int, budgetAllocation map[string]float64) *DefaultBudgetTracker {
	return budget.NewBudgetTracker(dailyLimit, budgetAllocation)
}

// NewCoordinator creates a new coordinator with default config.
func NewCoordinator(router Router, b BudgetTracker) *Coordinator {
	return budget.NewCoordinator(router, b)
}

// NewCoordinatorWithConfig creates a coordinator with custom config.
func NewCoordinatorWithConfig(
	router Router,
	b BudgetTracker,
	config CoordinatorConfig,
) *Coordinator {
	return budget.NewCoordinatorWithConfig(router, b, config)
}

// DefaultCoordinatorConfig returns sensible coordinator defaults.
func DefaultCoordinatorConfig() CoordinatorConfig {
	return budget.DefaultCoordinatorConfig()
}

// NewCoordinatedProvider creates a provider that uses coordination logic.
func NewCoordinatedProvider(
	chainID, chainName string,
	coordinator *Coordinator,
) *CoordinatedProvider {
	return budget.NewCoordinatedProvider(chainID, chainName, coordinator)
}
