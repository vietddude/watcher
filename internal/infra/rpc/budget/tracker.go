// Package budget handles RPC quota and rate limiting.
package budget

import (
	"log/slog"
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

// BudgetTracker interface.
type BudgetTracker interface {
	RecordCall(providerName, method string)
	GetProviderUsage(providerName string) UsageStats
	CanMakeCall(providerName string) bool
	GetThrottleDelay(providerName string) time.Duration
	GetUsagePercent() float64
	Reset()
}

type providerBudget struct {
	totalCalls     int
	callsThisHour  int
	hourStartTime  time.Time
	dailyLimit     int // 0 = unlimited
	windowLimit    int // requests per window (0 = unlimited)
	windowDuration time.Duration
	windowCalls    int
	windowStart    time.Time
}

// DefaultBudgetTracker implements BudgetTracker with per-provider tracking.
type DefaultBudgetTracker struct {
	mu            sync.Mutex
	providerUsage map[string]*providerBudget
	resetTime     time.Time
}

// NewBudgetTracker creates a new budget tracker.
func NewBudgetTracker() *DefaultBudgetTracker {
	now := time.Now()
	nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())

	return &DefaultBudgetTracker{
		providerUsage: make(map[string]*providerBudget),
		resetTime:     nextMidnight,
	}
}

// RecordCall records a call for quota tracking.
func (bt *DefaultBudgetTracker) RecordCall(providerName, method string) {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	now := time.Now()
	if now.After(bt.resetTime) {
		bt.resetUnsafe(now)
	}

	budget, ok := bt.providerUsage[providerName]
	if !ok {
		// Auto-initialize for unknown providers with unlimited quota
		budget = &providerBudget{
			dailyLimit:    0, // unlimited
			hourStartTime: now,
			windowStart:   now,
		}
		bt.providerUsage[providerName] = budget
	}

	if time.Since(budget.hourStartTime) >= time.Hour {
		budget.callsThisHour = 0
		budget.hourStartTime = now
	}

	// Window reset
	if budget.windowDuration > 0 && time.Since(budget.windowStart) >= budget.windowDuration {
		budget.windowCalls = 0
		budget.windowStart = now
	}

	budget.totalCalls++
	budget.callsThisHour++
	budget.windowCalls++
}

// GetProviderUsage returns usage statistics for a provider.
func (bt *DefaultBudgetTracker) GetProviderUsage(providerName string) UsageStats {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	budget, ok := bt.providerUsage[providerName]
	if !ok {
		return UsageStats{
			DailyLimit:      0, // unlimited
			RemainingCalls:  999999,
			UsagePercentage: 0,
			NextResetAt:     bt.resetTime,
		}
	}

	// Handle unlimited quota
	if budget.dailyLimit == 0 {
		return UsageStats{
			TotalCalls:      budget.totalCalls,
			CallsPerHour:    budget.callsThisHour,
			DailyLimit:      0,
			RemainingCalls:  999999, // Show as effectively unlimited
			UsagePercentage: 0,
			NextResetAt:     bt.resetTime,
		}
	}

	remaining := budget.dailyLimit - budget.totalCalls
	if remaining < 0 {
		remaining = 0
	}

	usagePercentage := float64(budget.totalCalls) / float64(budget.dailyLimit) * 100

	return UsageStats{
		TotalCalls:      budget.totalCalls,
		CallsPerHour:    budget.callsThisHour,
		DailyLimit:      budget.dailyLimit,
		RemainingCalls:  remaining,
		UsagePercentage: usagePercentage,
		NextResetAt:     bt.resetTime,
	}
}

// CanMakeCall checks if a call can be made within budget.
func (bt *DefaultBudgetTracker) CanMakeCall(providerName string) bool {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	budget, ok := bt.providerUsage[providerName]
	if !ok {
		return true // Allow if not tracked (will be initialized on RecordCall)
	}

	// 1. Daily Limit
	if budget.dailyLimit > 0 && budget.totalCalls >= budget.dailyLimit {
		return false
	}

	// 2. Window Limit
	if budget.windowLimit > 0 && budget.windowDuration > 0 {
		if time.Since(budget.windowStart) < budget.windowDuration {
			if budget.windowCalls >= budget.windowLimit {
				return false
			}
		}
	}

	return true
}

// GetThrottleDelay returns how long to wait before making a call.
func (bt *DefaultBudgetTracker) GetThrottleDelay(providerName string) time.Duration {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	budget, ok := bt.providerUsage[providerName]
	if !ok {
		return 0
	}

	// 1. Daily Quota throttle
	if budget.dailyLimit > 0 {
		usagePercentage := float64(budget.totalCalls) / float64(budget.dailyLimit) * 100
		if usagePercentage >= 100 {
			return time.Until(bt.resetTime)
		}
		if usagePercentage > 90 {
			return 2 * time.Second
		}
		if usagePercentage > 75 {
			return 500 * time.Millisecond
		}
	}

	// 2. Window-based throttle (Proactive)
	if budget.windowLimit > 0 && budget.windowDuration > 0 {
		windowUsage := float64(budget.windowCalls) / float64(budget.windowLimit)
		if windowUsage > 0.95 {
			delay := time.Until(budget.windowStart.Add(budget.windowDuration))
			slog.Warn("Rate limit reached, throttling", "provider", providerName, "windowCalls", budget.windowCalls, "limit", budget.windowLimit, "delay", delay)
			return delay
		}
		if windowUsage > 0.8 {
			return 200 * time.Millisecond
		}
	}

	return 0
}

// Reset resets all usage counters.
func (bt *DefaultBudgetTracker) Reset() {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	bt.resetUnsafe(time.Now())
}

func (bt *DefaultBudgetTracker) resetUnsafe(now time.Time) {
	for _, budget := range bt.providerUsage {
		budget.totalCalls = 0
		budget.callsThisHour = 0
		budget.hourStartTime = now
		budget.windowCalls = 0
		budget.windowStart = now
	}

	bt.resetTime = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
}

// GetUsagePercent returns the average usage percentage across all providers with limits.
func (bt *DefaultBudgetTracker) GetUsagePercent() float64 {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	totalCalls := 0
	totalLimit := 0
	for _, budget := range bt.providerUsage {
		if budget.dailyLimit > 0 {
			totalCalls += budget.totalCalls
			totalLimit += budget.dailyLimit
		}
	}

	if totalLimit == 0 {
		return 0
	}
	return float64(totalCalls) / float64(totalLimit) * 100
}

// SetProviderQuota sets the daily quota for a provider.
func (bt *DefaultBudgetTracker) SetProviderQuota(providerName string, quota int) {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	budget, ok := bt.providerUsage[providerName]
	if !ok {
		budget = &providerBudget{
			hourStartTime: time.Now(),
			windowStart:   time.Now(),
		}
		bt.providerUsage[providerName] = budget
	}
	budget.dailyLimit = quota
}

// SetProviderLimit sets the window-based limit.
func (bt *DefaultBudgetTracker) SetProviderLimit(providerName string, limit int, duration time.Duration) {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	budget, ok := bt.providerUsage[providerName]
	if !ok {
		budget = &providerBudget{
			hourStartTime: time.Now(),
			windowStart:   time.Now(),
		}
		bt.providerUsage[providerName] = budget
	}
	budget.windowLimit = limit
	budget.windowDuration = duration
}
