// Package budget handles RPC quota and rate limiting.
//
// This package contains:
//   - BudgetTracker: interface for quota management
//   - DefaultBudgetTracker: implementation with per-chain and per-provider tracking
//   - QuotaPredictor: predicts quota exhaustion based on request rate
//   - Coordinator: bridges budget and routing decisions
package budget

import (
	"sync"
	"time"
)

// UsageStats holds quota usage statistics.
type UsageStats struct {
	TotalCalls              int
	CallsPerHour            int
	DailyLimit              int
	RemainingCalls          int
	UsagePercentage         float64
	NextResetAt             time.Time
	PredictedExhaustionMins int
}

// Config holds budget configuration.
type Config struct {
	DailyQuota int
}

// BudgetTracker manages RPC quota and rate limiting.
type BudgetTracker interface {
	RecordCall(chainID, providerName, method string)
	GetUsage(chainID string) UsageStats
	GetProviderUsage(chainID, providerName string) UsageStats
	CanMakeCall(chainID string) bool
	CanUseProvider(chainID, providerName string) bool
	GetThrottleDelay(chainID string) time.Duration
	Reset()
}

type chainBudget struct {
	totalCalls          int
	callsThisHour       int
	hourStartTime       time.Time
	methodCalls         map[string]int
	providerCalls       map[string]int
	dailyAllocation     int
	providerAllocations map[string]int
}

// DefaultBudgetTracker implements BudgetTracker with per-chain and per-provider tracking.
type DefaultBudgetTracker struct {
	mu            sync.RWMutex
	chainUsage    map[string]*chainBudget
	dailyLimit    int
	resetTime     time.Time
	resetInterval time.Duration
}

// NewBudgetTracker creates a new budget tracker.
func NewBudgetTracker(dailyLimit int, budgetAllocation map[string]float64) *DefaultBudgetTracker {
	now := time.Now()
	nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())

	tracker := &DefaultBudgetTracker{
		chainUsage:    make(map[string]*chainBudget),
		dailyLimit:    dailyLimit,
		resetTime:     nextMidnight,
		resetInterval: 24 * time.Hour,
	}

	for chainID, percentage := range budgetAllocation {
		tracker.chainUsage[chainID] = &chainBudget{
			dailyAllocation:     int(float64(dailyLimit) * percentage),
			hourStartTime:       now,
			methodCalls:         make(map[string]int),
			providerCalls:       make(map[string]int),
			providerAllocations: make(map[string]int),
		}
	}

	return tracker
}

// RecordCall records a call for quota tracking.
func (bt *DefaultBudgetTracker) RecordCall(chainID, providerName, method string) {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	if time.Now().After(bt.resetTime) {
		bt.resetUnsafe()
	}

	budget, ok := bt.chainUsage[chainID]
	if !ok {
		budget = &chainBudget{
			dailyAllocation:     bt.dailyLimit / 10,
			hourStartTime:       time.Now(),
			methodCalls:         make(map[string]int),
			providerCalls:       make(map[string]int),
			providerAllocations: make(map[string]int),
		}
		bt.chainUsage[chainID] = budget
	}

	if time.Since(budget.hourStartTime) >= time.Hour {
		budget.callsThisHour = 0
		budget.hourStartTime = time.Now()
	}

	budget.totalCalls++
	budget.callsThisHour++
	budget.methodCalls[method]++
	budget.providerCalls[providerName]++
}

// GetUsage returns usage statistics for a chain.
func (bt *DefaultBudgetTracker) GetUsage(chainID string) UsageStats {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

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
	bt.mu.RLock()
	defer bt.mu.RUnlock()

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
	if providerAllocation == 0 {
		providerAllocation = budget.dailyAllocation / 5
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
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	budget, ok := bt.chainUsage[chainID]
	if !ok {
		return true
	}

	return budget.totalCalls < budget.dailyAllocation
}

// CanUseProvider checks if a specific provider has quota remaining.
func (bt *DefaultBudgetTracker) CanUseProvider(chainID, providerName string) bool {
	usage := bt.GetProviderUsage(chainID, providerName)
	return usage.UsagePercentage < 95
}

// GetThrottleDelay returns how long to wait before making a call.
func (bt *DefaultBudgetTracker) GetThrottleDelay(chainID string) time.Duration {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	_, ok := bt.chainUsage[chainID]
	if !ok {
		return 0
	}

	usage := bt.GetUsage(chainID)

	if usage.UsagePercentage < 50 {
		return 0
	}
	if usage.UsagePercentage < 70 {
		return 1 * time.Second
	}
	if usage.UsagePercentage < 90 {
		return 3 * time.Second
	}
	if usage.UsagePercentage < 100 {
		return 10 * time.Second
	}

	return time.Until(bt.resetTime)
}

// Reset resets all usage counters.
func (bt *DefaultBudgetTracker) Reset() {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	bt.resetUnsafe()
}

// SetProviderAllocation sets the daily allocation for a specific provider.
func (bt *DefaultBudgetTracker) SetProviderAllocation(chainID, providerName string, allocation int) {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	budget, ok := bt.chainUsage[chainID]
	if !ok {
		return
	}

	budget.providerAllocations[providerName] = allocation
}

func (bt *DefaultBudgetTracker) resetUnsafe() {
	for _, budget := range bt.chainUsage {
		budget.totalCalls = 0
		budget.callsThisHour = 0
		budget.hourStartTime = time.Now()
		budget.methodCalls = make(map[string]int)
		budget.providerCalls = make(map[string]int)
	}

	now := time.Now()
	bt.resetTime = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
}
