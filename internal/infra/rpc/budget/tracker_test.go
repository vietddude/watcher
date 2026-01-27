package budget

import (
	"sync"
	"testing"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
)

func TestBudgetTracker_Concurrency(t *testing.T) {
	tracker := NewBudgetTracker()
	tracker.SetProviderQuota("infura", 500)
	tracker.SetProviderQuota("alchemy", 500)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tracker.RecordCall("infura", "eth_blockNumber")
			tracker.CanMakeCall("infura")
			tracker.GetProviderUsage("infura")
		}()
	}
	wg.Wait()

	usage := tracker.GetProviderUsage("infura")
	if usage.TotalCalls != 100 {
		t.Errorf("Expected 100 calls, got %d", usage.TotalCalls)
	}
}

func TestBudgetTracker_Limits(t *testing.T) {
	tracker := NewBudgetTracker()
	tracker.SetProviderQuota("infura", 100)

	for i := 0; i < 100; i++ {
		if !tracker.CanMakeCall("infura") {
			t.Errorf("Should allow call %d", i)
		}
		tracker.RecordCall("infura", "eth_blockNumber")
	}

	if tracker.CanMakeCall("infura") {
		t.Error("Should deny call 101")
	}
}

func TestBudgetTracker_Throttle(t *testing.T) {
	tracker := NewBudgetTracker()
	tracker.SetProviderQuota("infura", 100)

	// 0% usage
	if delay := tracker.GetThrottleDelay(domain.EthereumMainnet, "infura"); delay != 0 {
		t.Error("Expected 0 delay")
	}

	// 80% usage (80 calls)
	for i := 0; i < 80; i++ {
		tracker.RecordCall("infura", "eth_blockNumber")
	}

	// Should have delay at 80%
	if delay := tracker.GetThrottleDelay(domain.EthereumMainnet, "infura"); delay == 0 {
		t.Error("Expected throttle delay at 80% usage")
	}
}

func TestBudgetTracker_UnlimitedQuota(t *testing.T) {
	tracker := NewBudgetTracker()
	tracker.SetProviderQuota("public", 0)

	// Should always allow
	for i := 0; i < 1000; i++ {
		if !tracker.CanMakeCall("public") {
			t.Error("Unlimited provider should always allow calls")
		}
		tracker.RecordCall("public", "eth_blockNumber")
	}

	// Usage percentage should be 0 for unlimited
	usage := tracker.GetProviderUsage("public")
	if usage.UsagePercentage != 0 {
		t.Errorf("Expected 0%% usage for unlimited, got %.1f%%", usage.UsagePercentage)
	}
}

func TestBudgetTracker_IntervalLimit(t *testing.T) {
	tracker := NewBudgetTracker()
	tracker.SetProviderLimit("limited", 5, 100*time.Millisecond)

	// Make 5 calls
	for i := 0; i < 5; i++ {
		if !tracker.CanMakeCall("limited") {
			t.Fatalf("Should allow call %d", i)
		}
		tracker.RecordCall("limited", "test")
	}

	// 6th call should be blocked
	if tracker.CanMakeCall("limited") {
		t.Error("Should block 6th call within window")
	}

	// Wait for window reset
	time.Sleep(150 * time.Millisecond)

	// Should allow again
	if !tracker.CanMakeCall("limited") {
		t.Error("Should allow calls after window reset")
	}
}
