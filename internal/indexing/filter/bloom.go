// Package filter provides address filtering implementations.
package filter

import (
	"hash/fnv"
	"strings"
	"sync"
)

// BloomFilter provides probabilistic address membership testing.
// It uses FNV-1a hashing with multiple hash functions for low false positive rates.
type BloomFilter struct {
	bits        []bool
	size        uint64
	hashes      int
	mu          sync.RWMutex
	initialized bool
}

// NewBloomFilter creates a new bloom filter.
// Default size is optimized for ~10,000 addresses with ~1% false positive rate.
func NewBloomFilter() *BloomFilter {
	return NewBloomFilterWithSize(100000, 7)
}

// NewBloomFilterWithSize creates a bloom filter with custom size and hash count.
// For n addresses with p false positive rate: size ≈ -n*ln(p)/(ln(2)^2), hashes = (size/n)*ln(2)
func NewBloomFilterWithSize(size uint64, hashes int) *BloomFilter {
	return &BloomFilter{
		bits:   make([]bool, size),
		size:   size,
		hashes: hashes,
	}
}

// Build initializes the filter with a set of addresses.
func (bf *BloomFilter) Build(addresses []string) {
	bf.mu.Lock()
	defer bf.mu.Unlock()

	// Clear existing bits
	bf.bits = make([]bool, bf.size)

	// Add all addresses
	for _, addr := range addresses {
		bf.addUnsafe(strings.ToLower(addr))
	}

	bf.initialized = true
}

// Add adds a single address to the filter.
func (bf *BloomFilter) Add(address string) {
	bf.mu.Lock()
	defer bf.mu.Unlock()
	bf.addUnsafe(strings.ToLower(address))
	bf.initialized = true
}

func (bf *BloomFilter) addUnsafe(address string) {
	for i := 0; i < bf.hashes; i++ {
		hash := bf.hash(address, i)
		bf.bits[hash%bf.size] = true
	}
}

// MayContain returns true if the address MIGHT be in the set.
// Returns false only if the address is DEFINITELY NOT in the set.
// False positives are possible, false negatives are not.
func (bf *BloomFilter) MayContain(address string) bool {
	bf.mu.RLock()
	defer bf.mu.RUnlock()

	if !bf.initialized {
		return true // If not initialized, assume everything might match
	}

	address = strings.ToLower(address)
	for i := 0; i < bf.hashes; i++ {
		hash := bf.hash(address, i)
		if !bf.bits[hash%bf.size] {
			return false
		}
	}
	return true
}

// IsInitialized returns true if Build() or Add() has been called.
func (bf *BloomFilter) IsInitialized() bool {
	bf.mu.RLock()
	defer bf.mu.RUnlock()
	return bf.initialized
}

// Clear resets the filter to empty state.
func (bf *BloomFilter) Clear() {
	bf.mu.Lock()
	defer bf.mu.Unlock()
	bf.bits = make([]bool, bf.size)
	bf.initialized = false
}

func (bf *BloomFilter) hash(value string, seed int) uint64 {
	h := fnv.New64a()
	h.Write([]byte(value))
	h.Write([]byte{byte(seed)})
	return h.Sum64()
}
