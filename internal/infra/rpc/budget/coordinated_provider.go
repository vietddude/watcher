package budget

import (
	"context"
	"fmt"
	"time"

	"github.com/vietddude/watcher/internal/indexing/metrics"
	"github.com/vietddude/watcher/internal/infra/rpc/provider"
)

// CoordinatedProvider wraps a Coordinator to implement the provider.Provider interface.
// This allows the EVMAdapter to use the failover/coordination logic transparently.
type CoordinatedProvider struct {
	chainID     string
	chainName   string // For metrics
	coordinator *Coordinator
}

// NewCoordinatedProvider creates a new provider that uses coordination logic.
func NewCoordinatedProvider(
	chainID, chainName string,
	coordinator *Coordinator,
) *CoordinatedProvider {
	return &CoordinatedProvider{
		chainID:     chainID,
		chainName:   chainName,
		coordinator: coordinator,
	}
}

// Call makes a coordinated RPC call with failover and budget checks.
func (p *CoordinatedProvider) Call(ctx context.Context, method string, params []any) (any, error) {
	start := time.Now()

	// Get provider name for metrics
	prov, _ := p.coordinator.GetBestProvider(p.chainID)
	providerName := "unknown"
	if prov != nil {
		providerName = prov.GetName()
	}

	result, err := p.coordinator.CallWithCoordination(ctx, p.chainID, method, params)

	// Record metrics
	duration := time.Since(start).Seconds()
	metrics.RPCCallsTotal.WithLabelValues(p.chainName, providerName, method).Inc()
	metrics.RPCLatency.WithLabelValues(p.chainName, providerName, method).Observe(duration)

	if err != nil {
		metrics.RPCErrorsTotal.WithLabelValues(p.chainName, providerName, "call_error").Inc()
	}

	return result, err
}

// BatchCall - Coordinated batch calls are not fully implemented yet, falling back to sequential calls or error.
func (p *CoordinatedProvider) BatchCall(
	ctx context.Context,
	requests []provider.BatchRequest,
) ([]provider.BatchResponse, error) {
	bestProvider, err := p.coordinator.GetBestProvider(p.chainID)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider for batch call: %w", err)
	}

	providerName := bestProvider.GetName()
	start := time.Now()

	rpcP, ok := bestProvider.(provider.RPCProvider)
	if !ok {
		return nil, fmt.Errorf("provider %s does not support batch calls", bestProvider.GetName())
	}

	result, err := rpcP.BatchCall(ctx, requests)

	// Record metrics
	duration := time.Since(start).Seconds()
	metrics.RPCCallsTotal.WithLabelValues(p.chainName, providerName, "batch").
		Add(float64(len(requests)))
	metrics.RPCLatency.WithLabelValues(p.chainName, providerName, "batch").Observe(duration)

	if err != nil {
		metrics.RPCErrorsTotal.WithLabelValues(p.chainName, providerName, "batch_error").Inc()
	}

	return result, err
}

// GetName returns a generic name as it represents multiple providers.
func (p *CoordinatedProvider) GetName() string {
	return "coordinated-provider-" + p.chainID
}

// GetHealth returns an aggregated or representative health status.
func (p *CoordinatedProvider) GetHealth() provider.HealthStatus {
	// For now, return healthy if we can get a provider
	prov, err := p.coordinator.GetBestProvider(p.chainID)
	if err != nil {
		return provider.HealthStatus{
			Available: false,
			ErrorRate: 1.0,
		}
	}
	// Return the health of the current best provider
	return prov.GetHealth()
}

// IsAvailable checks if the coordinated provider has any available underlying provider.
func (p *CoordinatedProvider) IsAvailable() bool {
	prov, err := p.coordinator.GetBestProvider(p.chainID)
	if err != nil {
		return false
	}
	return prov.IsAvailable()
}

// HasQuotaRemaining checks if the coordinated provider has quota.
func (p *CoordinatedProvider) HasQuotaRemaining() bool {
	prov, err := p.coordinator.GetBestProvider(p.chainID)
	if err != nil {
		return false
	}
	return prov.HasQuotaRemaining()
}

// HasCapacity checks if the coordinated provider has capacity for the given cost.
func (p *CoordinatedProvider) HasCapacity(cost int) bool {
	prov, err := p.coordinator.GetBestProvider(p.chainID)
	if err != nil {
		return false
	}
	return prov.HasCapacity(cost)
}

// Execute performs a coordinated operation with failover and monitoring.
func (p *CoordinatedProvider) Execute(ctx context.Context, op provider.Operation) (any, error) {
	start := time.Now()

	// Get provider name for metrics (before execution)
	prov, _ := p.coordinator.GetBestProvider(p.chainID)
	providerName := "unknown"
	if prov != nil {
		providerName = prov.GetName()
	}

	opName := op.Name
	if opName == "" {
		opName = "operation"
	}

	// Execute through coordinator (which handles failover AND budget tracking)
	result, err := p.coordinator.Execute(ctx, p.chainID, op)

	// Record metrics
	duration := time.Since(start).Seconds()
	metrics.RPCCallsTotal.WithLabelValues(p.chainName, providerName, opName).Inc()
	metrics.RPCLatency.WithLabelValues(p.chainName, providerName, opName).Observe(duration)

	if err != nil {
		metrics.RPCErrorsTotal.WithLabelValues(p.chainName, providerName, "execute_error").Inc()
	}

	return result, err
}

// Close closes the underlying coordinator (which might verify resources).
func (p *CoordinatedProvider) Close() error {
	return nil
}
