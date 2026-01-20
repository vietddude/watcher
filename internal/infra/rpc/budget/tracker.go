// Package budget handles RPC quota and rate limiting.
package budget

import (
	"sync"
	"time"
)

// UsageStats holds quota usage statistics.
type UsageStats struct {
	TotalCalls      int
	CallsPerHour    int
	DailyLimit      int
	RemainingCalls  int
	UsagePercentage float64
	NextResetAt     time.Time
}

// Config holds budget configuration.
type Config struct {
	DailyQuota int
}

// BudgetTracker interface.
type BudgetTracker interface {
	RecordCall(chainID, providerName, method string)
	GetUsage(chainID string) UsageStats
	GetProviderUsage(chainID, providerName string) UsageStats
	CanMakeCall(chainID string) bool
	CanUseProvider(chainID, providerName string) bool
	GetThrottleDelay(chainID string) time.Duration
	GetUsagePercent() float64
	Reset()
}

type chainBudget struct {
	totalCalls          int
	callsThisHour       int
	hourStartTime       time.Time
	dailyAllocation     int
	providerAllocations map[string]int
	providerCalls       map[string]int
}

// DefaultBudgetTracker implements BudgetTracker.
type DefaultBudgetTracker struct {
	mu         sync.Mutex // Changed to Mutex for simplicity and safety
	chainUsage map[string]*chainBudget
	dailyLimit int
	resetTime  time.Time
}

// NewBudgetTracker creates a new budget tracker.
func NewBudgetTracker(dailyLimit int, budgetAllocation map[string]float64) *DefaultBudgetTracker {
	now := time.Now()
	nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())

	tracker := &DefaultBudgetTracker{
		chainUsage: make(map[string]*chainBudget),
		dailyLimit: dailyLimit,
		resetTime:  nextMidnight,
	}

	for chainID, percentage := range budgetAllocation {
		tracker.chainUsage[chainID] = &chainBudget{
			dailyAllocation:     int(float64(dailyLimit) * percentage),
			hourStartTime:       now,
			providerAllocations: make(map[string]int),
			providerCalls:       make(map[string]int),
		}
	}

	return tracker
}

// RecordCall records a call for quota tracking.
func (bt *DefaultBudgetTracker) RecordCall(chainID, providerName, method string) {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	now := time.Now()
	if now.After(bt.resetTime) {
		bt.resetUnsafe(now)
	}

	budget, ok := bt.chainUsage[chainID]
	if !ok {
		// Auto-initialize for unknown chains with default 10% allocation
		budget = &chainBudget{
			dailyAllocation:     bt.dailyLimit / 10,
			hourStartTime:       now,
			providerAllocations: make(map[string]int),
			providerCalls:       make(map[string]int),
		}
		bt.chainUsage[chainID] = budget
	}

	if time.Since(budget.hourStartTime) >= time.Hour {
		budget.callsThisHour = 0
		budget.hourStartTime = now
	}

	budget.totalCalls++
	budget.callsThisHour++
	budget.providerCalls[providerName]++
}

// GetUsage returns usage statistics for a chain.
func (bt *DefaultBudgetTracker) GetUsage(chainID string) UsageStats {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	budget, ok := bt.chainUsage[chainID]
	if !ok {
		defaultLimit := bt.dailyLimit / 10
		return UsageStats{
			DailyLimit:      defaultLimit,
			RemainingCalls:  defaultLimit,
			UsagePercentage: 0,
			NextResetAt:     bt.resetTime,
		}
	}

	remaining := budget.dailyAllocation - budget.totalCalls
	if remaining < 0 {
		remaining = 0
	}

	usagePercentage := 0.0
	if budget.dailyAllocation > 0 {
		usagePercentage = float64(budget.totalCalls) / float64(budget.dailyAllocation) * 100
	}

	return UsageStats{
		TotalCalls:      budget.totalCalls,
		CallsPerHour:    budget.callsThisHour,
		DailyLimit:      budget.dailyAllocation,
		RemainingCalls:  remaining,
		UsagePercentage: usagePercentage,
		NextResetAt:     bt.resetTime,
	}
}

// GetProviderUsage returns usage statistics for a specific provider.
func (bt *DefaultBudgetTracker) GetProviderUsage(chainID, providerName string) UsageStats {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	budget, ok := bt.chainUsage[chainID]
	if !ok {
		return UsageStats{
			DailyLimit:      bt.dailyLimit / 10,
			RemainingCalls:  bt.dailyLimit / 10,
			UsagePercentage: 0,
			NextResetAt:     bt.resetTime,
		}
	}

	providerCalls := budget.providerCalls[providerName]
	providerAllocation := budget.providerAllocations[providerName]

	// Default provider allocation if not set
	if providerAllocation == 0 {
		providerAllocation = budget.dailyAllocation / 5 // Default: 20% of chain allocation
		if providerAllocation < 1000 {
			providerAllocation = 1000
		}
	}

	remaining := providerAllocation - providerCalls
	if remaining < 0 {
		remaining = 0
	}

	usagePercentage := 0.0
	if providerAllocation > 0 {
		usagePercentage = float64(providerCalls) / float64(providerAllocation) * 100
	}

	return UsageStats{
		TotalCalls:      providerCalls,
		DailyLimit:      providerAllocation,
		RemainingCalls:  remaining,
		UsagePercentage: usagePercentage,
		NextResetAt:     bt.resetTime,
	}
}

// CanMakeCall checks if a call can be made within budget.
func (bt *DefaultBudgetTracker) CanMakeCall(chainID string) bool {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	budget, ok := bt.chainUsage[chainID]
	if !ok {
		return true // Allow if not tracked yet (will be initialized on RecordCall)
	}

	return budget.totalCalls < budget.dailyAllocation
}

// CanUseProvider checks if a specific provider has quota remaining.
func (bt *DefaultBudgetTracker) CanUseProvider(chainID, providerName string) bool {
	// Re-using GetProviderUsage internal logic to avoid lock contention/double locking
	bt.mu.Lock()
	defer bt.mu.Unlock()

	budget, ok := bt.chainUsage[chainID]
	if !ok {
		return true
	}

	providerCalls := budget.providerCalls[providerName]
	providerAllocation := budget.providerAllocations[providerName]
	if providerAllocation == 0 {
		providerAllocation = budget.dailyAllocation / 5
		if providerAllocation < 1000 {
			providerAllocation = 1000
		}
	}

	if providerAllocation <= 0 {
		return true
	}

	usagePercentage := float64(providerCalls) / float64(providerAllocation) * 100
	return usagePercentage < 95
}

// GetThrottleDelay returns how long to wait before making a call.
func (bt *DefaultBudgetTracker) GetThrottleDelay(chainID string) time.Duration {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	budget, ok := bt.chainUsage[chainID]
	if !ok {
		return 0
	}

	// Simple thresholds based on daily usage
	usagePercentage := 0.0
	if budget.dailyAllocation > 0 {
		usagePercentage = float64(budget.totalCalls) / float64(budget.dailyAllocation) * 100
	}

	if usagePercentage < 50 {
		return 0
	}
	if usagePercentage < 70 {
		return 100 * time.Millisecond
	}
	if usagePercentage < 90 {
		return 500 * time.Millisecond
	}
	if usagePercentage < 100 {
		return 2 * time.Second
	}

	return time.Until(bt.resetTime)
}

// Reset resets all usage counters.
func (bt *DefaultBudgetTracker) Reset() {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	bt.resetUnsafe(time.Now())
}

// SetProviderAllocation sets the daily allocation for a specific provider.
func (bt *DefaultBudgetTracker) SetProviderAllocation(
	chainID, providerName string,
	allocation int,
) {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	budget, ok := bt.chainUsage[chainID]
	if !ok {
		return
	}

	budget.providerAllocations[providerName] = allocation
}

func (bt *DefaultBudgetTracker) resetUnsafe(now time.Time) {
	for _, budget := range bt.chainUsage {
		budget.totalCalls = 0
		budget.callsThisHour = 0
		budget.hourStartTime = now
		budget.providerCalls = make(map[string]int)
	}

	bt.resetTime = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
}

// GetUsagePercent returns the overall usage percentage across all chains.
func (bt *DefaultBudgetTracker) GetUsagePercent() float64 {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	totalCalls := 0
	for _, budget := range bt.chainUsage {
		totalCalls += budget.totalCalls
	}

	if bt.dailyLimit == 0 {
		return 0
	}
	return float64(totalCalls) / float64(bt.dailyLimit) * 100
}
