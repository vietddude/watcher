package budget

import (
	"sync"
	"testing"
)

func TestBudgetTracker_Concurrency(t *testing.T) {
	allocation := map[string]float64{
		"eth": 0.5,
		"bsc": 0.5,
	}
	tracker := NewBudgetTracker(1000, allocation)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tracker.RecordCall("eth", "infura", "eth_blockNumber")
			tracker.CanMakeCall("eth")
			tracker.GetUsage("eth")
		}()
	}
	wg.Wait()

	usage := tracker.GetUsage("eth")
	if usage.TotalCalls != 100 {
		t.Errorf("Expected 100 calls, got %d", usage.TotalCalls)
	}
}

func TestBudgetTracker_Limits(t *testing.T) {
	allocation := map[string]float64{
		"eth": 0.1, // 100 calls
	}
	tracker := NewBudgetTracker(1000, allocation)

	for i := 0; i < 100; i++ {
		if !tracker.CanMakeCall("eth") {
			t.Errorf("Should allow call %d", i)
		}
		tracker.RecordCall("eth", "infura", "eth_blockNumber")
	}

	if tracker.CanMakeCall("eth") {
		t.Error("Should deny call 101")
	}
}

func TestBudgetTracker_Throttle(t *testing.T) {
	allocation := map[string]float64{
		"eth": 0.1, // 100 calls
	}
	tracker := NewBudgetTracker(1000, allocation)

	// 0% usage
	if delay := tracker.GetThrottleDelay("eth"); delay != 0 {
		t.Error("Expected 0 delay")
	}

	// 80% usage (80 calls)
	for i := 0; i < 80; i++ {
		tracker.RecordCall("eth", "infura", "eth_blockNumber")
	}

	// Should have delay
	if delay := tracker.GetThrottleDelay("eth"); delay == 0 {
		t.Error("Expected throttle delay at 80% usage")
	}
}
