package provider

import (
	"testing"
	"time"
)

func TestMonitorOptimization(t *testing.T) {
	m := NewProviderMonitor()

	// Add requests
	m.RecordRequest(100 * time.Millisecond)

	stats := m.GetStats()
	if stats.RequestsLast24Hours != 1 {
		t.Errorf("Expected 1 request, got %d", stats.RequestsLast24Hours)
	}

	// Test accumulation
	// Simulate multiple requests
	for i := 0; i < 100; i++ {
		m.RecordRequest(50 * time.Millisecond)
	}

	stats = m.GetStats()
	if stats.RequestsLast24Hours != 101 {
		t.Errorf("Expected 101 requests, got %d", stats.RequestsLast24Hours)
	}
}

func TestMonitorSlidingWindowApprox(t *testing.T) {
	m := NewProviderMonitor()

	// Record 1 request
	m.RecordRequest(100 * time.Millisecond)

	count := m.GetRequestCount(1 * time.Minute)
	if count != 1 {
		t.Errorf("Expected 1 request count, got %d", count)
	}

	// Note: We cannot easily test time passing without mocking time.Now()
	// or waiting minutes in test (which is bad).
	// We rely on the unit test above to verify basic accumulation.
}
