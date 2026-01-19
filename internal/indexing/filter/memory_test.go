package filter

import (
	"context"
	"testing"
)

func TestMemoryFilter(t *testing.T) {
	f := NewMemoryFilter()

	// Test Add and Contains
	f.Add("0x123")
	if !f.Contains("0x123") {
		t.Error("Expected filter to contain 0x123")
	}
	if !f.Contains("0X123") {
		t.Error("Expected filter to be case-insensitive")
	}
	if f.Contains("0x456") {
		t.Error("Expected filter not to contain 0x456")
	}

	// Test AddBatch
	f.AddBatch([]string{"0xabc", "0xdef"})
	if !f.Contains("0xabc") || !f.Contains("0xdef") {
		t.Error("Expected filter to contain batch added addresses")
	}

	// Test Size
	if f.Size() != 3 {
		t.Errorf("Expected size to be 3, got %d", f.Size())
	}

	// Test Remove
	f.Remove("0x123")
	if f.Contains("0x123") {
		t.Error("Expected filter not to contain 0x123 after removal")
	}
	if f.Size() != 2 {
		t.Errorf("Expected size to be 2, got %d", f.Size())
	}

	// Test Addresses
	addrs := f.Addresses()
	if len(addrs) != 2 {
		t.Errorf("Expected 2 addresses, got %d", len(addrs))
	}

	// Rebuild (should be no-op but no error)
	if err := f.Rebuild(context.Background()); err != nil {
		t.Errorf("Rebuild failed: %v", err)
	}
}
