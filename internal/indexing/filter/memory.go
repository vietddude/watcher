package filter

import (
	"context"
	"strings"
	"sync"
)

// MemoryFilter implements Filter interface using an in-memory map.
type MemoryFilter struct {
	addresses map[string]struct{}
	mu        sync.RWMutex
}

// NewMemoryFilter creates a new in-memory filter.
func NewMemoryFilter() *MemoryFilter {
	return &MemoryFilter{
		addresses: make(map[string]struct{}),
	}
}

// Contains checks if an address is tracked.
func (f *MemoryFilter) Contains(address string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, exists := f.addresses[strings.ToLower(address)]
	return exists
}

// Add adds an address to the filter.
func (f *MemoryFilter) Add(address string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addresses[strings.ToLower(address)] = struct{}{}
	return nil
}

// AddBatch adds multiple addresses.
func (f *MemoryFilter) AddBatch(addresses []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, addr := range addresses {
		f.addresses[strings.ToLower(addr)] = struct{}{}
	}
	return nil
}

// Remove removes an address from the filter.
func (f *MemoryFilter) Remove(address string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.addresses, strings.ToLower(address))
	return nil
}

// Size returns the number of tracked addresses.
func (f *MemoryFilter) Size() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.addresses)
}

// Rebuild rebuilds the filter (no-op for map-based filter).
func (f *MemoryFilter) Rebuild(ctx context.Context) error {
	return nil
}

// Addresses returns the list of all tracked addresses.
func (f *MemoryFilter) Addresses() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	result := make([]string, 0, len(f.addresses))
	for addr := range f.addresses {
		result = append(result, addr)
	}
	return result
}
