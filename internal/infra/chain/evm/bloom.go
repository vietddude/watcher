package evm

import (
	"hash/fnv"
	"strings"
	"sync"
)

type BloomFilter struct {
	bits        []bool
	size        uint64
	hashes      int
	mu          sync.RWMutex
	initialized bool
}

func NewBloomFilter() *BloomFilter {
	// Size optimized for ~10,000 addresses with 1% false positive rate
	size := uint64(100000)
	hashes := 7

	return &BloomFilter{
		bits:   make([]bool, size),
		size:   size,
		hashes: hashes,
	}
}

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

func (bf *BloomFilter) Add(address string) {
	bf.mu.Lock()
	defer bf.mu.Unlock()
	bf.addUnsafe(strings.ToLower(address))
}

func (bf *BloomFilter) addUnsafe(address string) {
	for i := 0; i < bf.hashes; i++ {
		hash := bf.hash(address, i)
		bf.bits[hash%bf.size] = true
	}
}

func (bf *BloomFilter) MayContain(address string) bool {
	bf.mu.RLock()
	defer bf.mu.RUnlock()

	address = strings.ToLower(address)
	for i := 0; i < bf.hashes; i++ {
		hash := bf.hash(address, i)
		if !bf.bits[hash%bf.size] {
			return false
		}
	}
	return true
}

func (bf *BloomFilter) IsInitialized() bool {
	bf.mu.RLock()
	defer bf.mu.RUnlock()
	return bf.initialized
}

func (bf *BloomFilter) hash(value string, seed int) uint64 {
	h := fnv.New64a()
	h.Write([]byte(value))
	h.Write([]byte{byte(seed)})
	return h.Sum64()
}
