package filter

import "context"

// Filter defines the interface for address filtering
type Filter interface {
	// Contains checks if an address is tracked
	Contains(address string) bool

	// Add adds an address to the filter
	Add(address string) error

	// AddBatch adds multiple addresses
	AddBatch(addresses []string) error

	// Remove removes an address from the filter
	Remove(address string) error

	// Size returns the number of tracked addresses
	Size() int

	// Rebuild rebuilds the filter (for bloom filters)
	Rebuild(ctx context.Context) error
}
