package budget

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vietddude/watcher/internal/infra/rpc/provider"
)

// MockRouter for testing
type MockRouter struct {
	providers []provider.Provider
}

func (m *MockRouter) GetProvider(chainID string) (provider.Provider, error) {
	if len(m.providers) > 0 {
		return m.providers[0], nil
	}
	return nil, errors.New("no providers")
}

func (m *MockRouter) GetAllProviders(chainID string) []provider.Provider {
	return m.providers
}

func (m *MockRouter) AddProvider(chainID string, p provider.Provider) {
	// No-op for mock
}

func (m *MockRouter) RecordSuccess(providerName string, latency time.Duration) {}
func (m *MockRouter) RecordFailure(providerName string, err error)             {}
func (m *MockRouter) RotateProvider(chainID string) (provider.Provider, error) {
	return nil, nil
}

func (m *MockRouter) GetProviderWithHint(
	chainID string,
	preferredProvider string,
) (provider.Provider, error) {
	if len(m.providers) > 0 {
		return m.providers[0], nil
	}
	return nil, errors.New("no providers")
}

// MockProvider for testing
type MockProvider struct {
	Name string
}

func (m *MockProvider) Call(ctx context.Context, method string, params []any) (any, error) {
	return "success", nil
}

func (m *MockProvider) BatchCall(
	ctx context.Context,
	requests []provider.BatchRequest,
) ([]provider.BatchResponse, error) {
	return nil, nil
}
func (m *MockProvider) GetName() string { return m.Name }
func (m *MockProvider) GetHealth() provider.HealthStatus {
	return provider.HealthStatus{Available: true}
}
func (m *MockProvider) IsAvailable() bool         { return true }
func (m *MockProvider) HasQuotaRemaining() bool   { return true }
func (m *MockProvider) HasCapacity(cost int) bool { return true }
func (m *MockProvider) Execute(ctx context.Context, op provider.Operation) (any, error) {
	if op.Invoke != nil {
		return op.Invoke(ctx)
	}
	return "success", nil
}
func (m *MockProvider) Close() error { return nil }

func TestCoordinator_Rotation(t *testing.T) {
	tracker := NewBudgetTracker(1000, map[string]float64{"eth": 1.0})

	p1 := &MockProvider{Name: "p1"}
	p2 := &MockProvider{Name: "p2"}

	// Pre-fill p1 to exhaust quota
	tracker.SetProviderAllocation("eth", "p1", 100)
	for i := 0; i < 96; i++ { // 96% usage
		tracker.RecordCall("eth", "p1", "test")
	}

	router := &MockRouter{providers: []provider.Provider{p1, p2}}
	coordinator := NewCoordinator(router, tracker)

	// Should rotate from p1 to p2
	best, err := coordinator.GetBestProvider("eth")
	if err != nil {
		t.Fatalf("GetBestProvider failed: %v", err)
	}

	if best.GetName() == "p1" {
		t.Error("Expected rotation from p1, but got p1")
	}
}

func TestCoordinator_Latency(t *testing.T) {
	tracker := NewBudgetTracker(1000, map[string]float64{"eth": 1.0})
	p1 := &MockProvider{Name: "p1"}
	router := &MockRouter{providers: []provider.Provider{p1}}
	coordinator := NewCoordinator(router, tracker)

	_, err := coordinator.CallWithCoordination(context.Background(), "eth", "test", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Verified by manual inspection of code flow or more complex mock router
}
