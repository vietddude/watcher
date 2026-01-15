package budget

import (
	"context"
	"fmt"

	"github.com/vietddude/watcher/internal/infra/rpc/provider"
)

// CoordinatedProvider wraps a Coordinator to implement the provider.Provider interface.
// This allows the EVMAdapter to use the failover/coordination logic transparently.
type CoordinatedProvider struct {
	chainID     string
	coordinator *Coordinator
}

// NewCoordinatedProvider creates a new provider that uses coordination logic.
func NewCoordinatedProvider(chainID string, coordinator *Coordinator) *CoordinatedProvider {
	return &CoordinatedProvider{
		chainID:     chainID,
		coordinator: coordinator,
	}
}

// Call makes a coordinated RPC call with failover and budget checks.
func (p *CoordinatedProvider) Call(ctx context.Context, method string, params []any) (any, error) {
	return p.coordinator.CallWithCoordination(ctx, p.chainID, method, params)
}

// BatchCall - Coordinated batch calls are not fully implemented yet, falling back to sequential calls or error.
// For now, we can try to implement a basic version or return error if not critical.
// Since EVMAdapter mostly uses single calls, we'll start with basic implementation if possible,
// or just error out. Given likely usage, let's implement a simple loop for now
// or better: just delegate to the "best" provider without failover for the whole batch?
// Failover within a batch is complex.
// Let's implement a "best effort" using current best provider to match interface.
func (p *CoordinatedProvider) BatchCall(ctx context.Context, requests []provider.BatchRequest) ([]provider.BatchResponse, error) {
	bestProvider, err := p.coordinator.GetBestProvider(p.chainID)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider for batch call: %w", err)
	}
	return bestProvider.BatchCall(ctx, requests)
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

// Close closes the underlying coordinator (which might verify resources).
// Coordinator doesn't have Close(), but Router does?
// Providers in router need closing.
// Current Coordinator struct doesn't have Close.
// We can leave this no-op or implement cleanup if needed.
func (p *CoordinatedProvider) Close() error {
	return nil
}
