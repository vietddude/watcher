package rpc

import (
	"context"
	"fmt"
	"testing"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/rpc/provider"
)

// TestChain is a mock chain ID for unit testing
const TestChain = domain.ChainID("999")

// MockProvider implements provider.Provider for routing tests
type MockProvider struct {
	name       string
	shouldFail bool
	callCount  int
}

func (m *MockProvider) GetName() string {
	return m.name
}

func (m *MockProvider) Call(ctx context.Context, method string, params []any) (any, error) {
	m.callCount++
	if m.shouldFail {
		return nil, fmt.Errorf("mock provider %s failed", m.name)
	}
	return "success_result", nil
}

func (m *MockProvider) BatchCall(
	ctx context.Context,
	requests []provider.BatchRequest,
) ([]provider.BatchResponse, error) {
	return nil, nil
}

func (m *MockProvider) Execute(ctx context.Context, op provider.Operation) (any, error) {
	m.callCount++
	if m.shouldFail {
		return nil, fmt.Errorf("mock provider %s failed", m.name)
	}
	if op.Invoke != nil {
		return op.Invoke(ctx)
	}
	return "success_result", nil
}

func (m *MockProvider) GetHealth() provider.HealthStatus {
	return provider.HealthStatus{
		Available: !m.shouldFail,
	}
}

func (m *MockProvider) IsAvailable() bool {
	return !m.shouldFail
}

func (m *MockProvider) HasQuotaRemaining() bool {
	return true
}

func (m *MockProvider) HasCapacity(cost int) bool {
	return true
}

func (m *MockProvider) Close() error {
	return nil
}

// TestRPC_RetryAndFailover verifies retry on primary provider
// and failover to secondary provider
func TestRPC_RetryAndFailover(t *testing.T) {
	ctx := context.Background()

	// Primary provider always fails
	primary := &MockProvider{
		name:       "primary",
		shouldFail: true,
	}

	// Secondary provider succeeds
	secondary := &MockProvider{
		name:       "secondary",
		shouldFail: false,
	}

	// Budget tracker (permissive for test)
	budget := NewBudgetTracker(
		1000,
		map[domain.ChainID]float64{
			TestChain: 1.0,
		},
	)

	// Router setup
	router := NewRouter(budget)
	router.AddProvider(TestChain, primary)
	router.AddProvider(TestChain, secondary)

	// Client
	client := NewClient(TestChain, router, budget)

	// Execute RPC call
	result, err := client.Call(ctx, "test_method", nil)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if result != "success_result" {
		t.Fatalf("unexpected result: %v", result)
	}

	// Assertions
	if primary.callCount != DefaultRetryConfig.MaxAttempts {
		t.Errorf(
			"primary provider expected %d retries, got %d",
			DefaultRetryConfig.MaxAttempts,
			primary.callCount,
		)
	}

	if secondary.callCount != 1 {
		t.Errorf("secondary provider expected 1 call, got %d", secondary.callCount)
	}
}
