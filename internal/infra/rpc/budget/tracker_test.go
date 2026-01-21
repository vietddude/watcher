package budget

import (
	"sync"
	"testing"

	"github.com/vietddude/watcher/internal/core/domain"
)

func TestBudgetTracker_Concurrency(t *testing.T) {
	allocation := map[domain.ChainID]float64{
		domain.EthereumMainnet: 0.5,
		domain.PolygonMainnet:  0.5,
	}
	tracker := NewBudgetTracker(1000, allocation)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tracker.RecordCall(domain.EthereumMainnet, "infura", "eth_blockNumber")
			tracker.CanMakeCall(domain.EthereumMainnet)
			tracker.GetUsage(domain.EthereumMainnet)
		}()
	}
	wg.Wait()

	usage := tracker.GetUsage(domain.EthereumMainnet)
	if usage.TotalCalls != 100 {
		t.Errorf("Expected 100 calls, got %d", usage.TotalCalls)
	}
}

func TestBudgetTracker_Limits(t *testing.T) {
	allocation := map[domain.ChainID]float64{
		domain.EthereumMainnet: 0.1, // 100 calls
	}
	tracker := NewBudgetTracker(1000, allocation)

	for i := 0; i < 100; i++ {
		if !tracker.CanMakeCall(domain.EthereumMainnet) {
			t.Errorf("Should allow call %d", i)
		}
		tracker.RecordCall(domain.EthereumMainnet, "infura", "eth_blockNumber")
	}

	if tracker.CanMakeCall(domain.EthereumMainnet) {
		t.Error("Should deny call 101")
	}
}

func TestBudgetTracker_Throttle(t *testing.T) {
	allocation := map[domain.ChainID]float64{
		domain.EthereumMainnet: 0.1, // 100 calls
	}
	tracker := NewBudgetTracker(1000, allocation)

	// 0% usage
	if delay := tracker.GetThrottleDelay(domain.EthereumMainnet); delay != 0 {
		t.Error("Expected 0 delay")
	}

	// 80% usage (80 calls)
	for range 80 {
		tracker.RecordCall(domain.EthereumMainnet, "infura", "eth_blockNumber")
	}

	// Should have delay
	if delay := tracker.GetThrottleDelay(domain.EthereumMainnet); delay == 0 {
		t.Error("Expected throttle delay at 80% usage")
	}
}
