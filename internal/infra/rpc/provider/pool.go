package provider

import (
	"fmt"
	"sync"
	"time"
)

// ProviderPool manages a pool of provider connections with health checks.
type ProviderPool struct {
	mu sync.RWMutex

	providers []Provider
	current   int
	maxSize   int

	healthCheckInterval time.Duration
	stopHealthCheck     chan struct{}
}

// NewProviderPool creates a new provider pool.
func NewProviderPool(maxSize int, healthCheckInterval time.Duration) *ProviderPool {
	pool := &ProviderPool{
		providers:           make([]Provider, 0, maxSize),
		maxSize:             maxSize,
		healthCheckInterval: healthCheckInterval,
		stopHealthCheck:     make(chan struct{}),
	}

	go pool.healthCheckLoop()

	return pool
}

// Add adds a provider to the pool.
func (p *ProviderPool) Add(provider Provider) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.providers) >= p.maxSize {
		return fmt.Errorf("pool is full")
	}

	p.providers = append(p.providers, provider)
	return nil
}

// Get returns the next available provider (round-robin).
func (p *ProviderPool) Get() (Provider, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.providers) == 0 {
		return nil, fmt.Errorf("no providers in pool")
	}

	provider := p.providers[p.current]
	p.current = (p.current + 1) % len(p.providers)

	return provider, nil
}

// Close closes all providers and stops health checks.
func (p *ProviderPool) Close() error {
	close(p.stopHealthCheck)

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, provider := range p.providers {
		if err := provider.Close(); err != nil {
			return err
		}
	}

	return nil
}

func (p *ProviderPool) healthCheckLoop() {
	ticker := time.NewTicker(p.healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.performHealthCheck()
		case <-p.stopHealthCheck:
			return
		}
	}
}

func (p *ProviderPool) performHealthCheck() {
	p.mu.RLock()
	providers := make([]Provider, len(p.providers))
	copy(providers, p.providers)
	p.mu.RUnlock()

	for _, provider := range providers {
		health := provider.GetHealth()
		// Could log or trigger alerts for unhealthy providers
		_ = health
	}
}
