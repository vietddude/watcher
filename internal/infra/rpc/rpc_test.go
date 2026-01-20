package rpc

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/vietddude/watcher/internal/infra/rpc/provider"
)

func TestRPC(t *testing.T) {
	err := godotenv.Load("../../../.env")
	if err != nil {
		t.Fatal("No .env file found")
	}

	INFURA_URL := os.Getenv("INFURA_URL")
	ALCHEMY_URL := os.Getenv("ALCHEMY_URL")
	if INFURA_URL == "" {
		t.Fatal("INFURA_URL is not set")
	}
	if ALCHEMY_URL == "" {
		t.Fatal("ALCHEMY_URL is not set")
	}

	ctx := context.Background()

	// 1. Create providers
	provider1 := NewHTTPProvider("alchemy", ALCHEMY_URL, 30*time.Second)
	provider2 := NewHTTPProvider("infura", INFURA_URL, 30*time.Second)

	// 2. Setup budget tracker
	budgetAllocation := map[string]float64{
		"ethereum": 1.0,
	}
	budget := NewBudgetTracker(100000, budgetAllocation)

	// 3. Setup router with proactive rotation strategy
	router := NewRouterWithStrategy(budget, RotationProactive)
	router.AddProvider("ethereum", provider1)
	router.AddProvider("ethereum", provider2)

	// 4. Create coordinator for unified budget-router coordination
	coordinator := NewCoordinator(router, budget)

	// Set up rotation callback to see when rotations happen
	coordinator.SetRotationCallback(func(chainID, from, to, reason string) {
		t.Logf("🔄 Rotated from %s to %s: %s\n", from, to, reason)
	})

	// 5. Create client with coordinator (full features)
	client := NewClientWithCoordinator("ethereum", coordinator)

	// 6. Make multiple calls to test rotation and prediction
	for i := 0; i < 5; i++ {
		result, err := client.Call(ctx, "eth_blockNumber", []any{})
		if err != nil {
			t.Errorf("Call %d failed: %v", i+1, err)
			continue
		}
		t.Logf("Call %d: Block = %s\n", i+1, result.(string))
	}

	// 8. Print monitor dashboard
	t.Logf("%s", client.PrintMonitorDashboard())

	// 9. Show budget usage
	usage := client.GetUsage()
	t.Logf("Total calls made: %d / %d (%.1f%%)\n",
		usage.TotalCalls, usage.DailyLimit, usage.UsagePercentage)
}

// MockProvider implements Provider interface for testing
type MockProvider struct {
	name       string
	shouldFail bool
	callCount  int
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
	requests []BatchRequest,
) ([]BatchResponse, error) {
	return nil, nil
}

func (m *MockProvider) GetName() string {
	return m.name
}

func (m *MockProvider) GetHealth() HealthStatus {
	return HealthStatus{Available: !m.shouldFail}
}

func (m *MockProvider) Close() error {
	return nil
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

func (m *MockProvider) Execute(ctx context.Context, op provider.Operation) (any, error) {
	m.callCount++
	if m.shouldFail {
		return nil, fmt.Errorf("mock provider %s failed", m.name)
	}
	// If Invoke is set, use it; otherwise return success
	if op.Invoke != nil {
		return op.Invoke(ctx)
	}
	return "success_result", nil
}

func TestRPCFallback(t *testing.T) {
	// 1. Setup Mock Providers
	// Primary: Fails
	p1 := &MockProvider{name: "primary", shouldFail: true}
	// Secondary: Succeeds
	p2 := &MockProvider{name: "secondary", shouldFail: false}

	// 2. Setup Budget (generic)
	budget := NewBudgetTracker(1000, map[string]float64{"test-chain": 1.0})

	// 3. Setup Router
	router := NewRouter(budget)
	router.AddProvider("test-chain", p1)
	router.AddProvider("test-chain", p2)

	// 4. Create Client
	client := NewClient("test-chain", router, budget)

	// 5. Execute CallWithFailover
	// Note: CallWithFailover uses CallWithRetryAndFailover, which uses DefaultRetryConfig.
	// DefaultRetryConfig has MaxAttempts: 5.
	// So p1 should be called 5 times, then fail.
	// Then p2 should be called 1 time and succeed.
	ctx := context.Background()
	result, err := client.Call(ctx, "test_method", nil)

	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	if result != "success_result" {
		t.Errorf("Expected 'success_result', got %v", result)
	}

	// 6. Verify Call Counts
	if p1.callCount != 5 {
		t.Errorf("Expected primary to be called 5 times (due to retry), got %d", p1.callCount)
	}
	if p2.callCount != 1 {
		t.Errorf("Expected secondary to be called 1 time, got %d", p2.callCount)
	}
}
