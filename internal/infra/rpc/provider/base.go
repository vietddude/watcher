package provider

import (
	"sync"
	"time"
)

// BaseProvider implements common provider functionality.
// It handles health tracking, metrics, and basic status checks.
type BaseProvider struct {
	Name string

	mu           sync.RWMutex
	health       HealthStatus
	totalLatency time.Duration
	successCount int
	failureCount int
	requestCount int

	Monitor *ProviderMonitor
}

// NewBaseProvider creates a new BaseProvider.
func NewBaseProvider(name string) *BaseProvider {
	return &BaseProvider{
		Name: name,
		health: HealthStatus{
			Available:     true,
			LastSuccessAt: time.Now(),
		},
		Monitor: NewProviderMonitor(),
	}
}

// GetName returns the provider's name.
func (p *BaseProvider) GetName() string {
	return p.Name
}

// GetHealth returns the provider's health status.
func (p *BaseProvider) GetHealth() HealthStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.health
}

// IsAvailable checks if the provider is available.
func (p *BaseProvider) IsAvailable() bool {
	status := p.Monitor.CheckProviderStatus()
	return status == StatusHealthy || status == StatusDegraded
}

// HasQuotaRemaining checks if this provider has quota remaining.
func (p *BaseProvider) HasQuotaRemaining() bool {
	status := p.Monitor.CheckProviderStatus()
	if status == StatusThrottled || status == StatusBlocked {
		return false
	}

	stats := p.Monitor.GetStats()
	return stats.UsagePercentage < 95
}

// GetUsagePercentage returns the current usage percentage.
func (p *BaseProvider) GetUsagePercentage() float64 {
	return p.Monitor.GetStats().UsagePercentage
}

func (p *BaseProvider) RecordSuccess(latency time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.successCount++
	p.requestCount++
	p.totalLatency += latency
	p.health.LastSuccessAt = time.Now()
	p.health.Available = true

	if p.requestCount > 0 {
		p.health.ErrorRate = float64(p.failureCount) / float64(p.requestCount)
	}
	if p.successCount > 0 {
		p.health.Latency = p.totalLatency / time.Duration(p.successCount)
	}

	// Also update monitor
	p.Monitor.RecordRequest(latency)
}

func (p *BaseProvider) RecordFailure() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.failureCount++
	p.requestCount++
	p.health.LastFailureAt = time.Now()

	if p.requestCount > 0 {
		p.health.ErrorRate = float64(p.failureCount) / float64(p.requestCount)
	}

	if p.health.ErrorRate > 0.5 {
		p.health.Available = false
	}
}

// HasCapacity checks if the provider has capacity for the given cost.
func (p *BaseProvider) HasCapacity(cost int) bool {
	if cost <= 0 {
		cost = 1
	}

	status := p.Monitor.CheckProviderStatus()
	if status == StatusThrottled || status == StatusBlocked {
		return false
	}

	stats := p.Monitor.GetStats()
	// Check if we have enough headroom for this cost
	return stats.UsagePercentage < 95
}
