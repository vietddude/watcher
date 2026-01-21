package provider

import (
	"strings"
	"sync"
	"time"
)

const dayInMinutes = 1440

// ProviderStatus represents the health state of a provider.
type ProviderStatus int

const (
	StatusHealthy   ProviderStatus = iota // Provider is working normally
	StatusDegraded                        // Provider is slow but working
	StatusThrottled                       // Provider is rate limiting
	StatusBlocked                         // Provider has blocked this client
)

// MonitorStats holds monitoring statistics for a provider.
type MonitorStats struct {
	Status              ProviderStatus
	AverageLatency      time.Duration
	ThrottleCount429    int
	ThrottleCount403    int
	RequestsLast1Hour   int
	RequestsLast24Hours int
	EstimatedDailyLimit int
	UsagePercentage     float64
}

// ProviderMonitor tracks provider health and rate limiting.
type ProviderMonitor struct {
	mu sync.RWMutex

	// Response time tracking
	recentLatencies  []time.Duration
	maxLatencyWindow int

	// Error tracking
	status429Count     int
	status403Count     int
	throttlePatterns   []string
	lastThrottleTime   time.Time
	retryAfterDuration time.Duration

	// Sliding window (Buckets for last 24h in minutes)
	buckets     [dayInMinutes]int
	bucketTimes [dayInMinutes]int64

	EstimatedDailyLimit int
	windowDuration      time.Duration

	// Thresholds
	slowResponseThreshold time.Duration
	degradedThreshold     float64
}

// NewProviderMonitor creates a new monitor with default settings.
func NewProviderMonitor() *ProviderMonitor {
	return &ProviderMonitor{
		recentLatencies:  make([]time.Duration, 0, 100),
		maxLatencyWindow: 100,
		throttlePatterns: []string{
			"rate limit exceeded",
			"too many requests",
			"daily request count exceeded",
			"project rate limit",
			"monthly quota exceeded",
		},
		EstimatedDailyLimit:   100000, // Conservative estimate
		windowDuration:        24 * time.Hour,
		slowResponseThreshold: 3 * time.Second,
		degradedThreshold:     0.3, // 30% error rate
	}
}

// RecordRequest records a successful request with its latency.
func (pm *ProviderMonitor) RecordRequest(latency time.Duration) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Track latency
	pm.recentLatencies = append(pm.recentLatencies, latency)
	if len(pm.recentLatencies) > pm.maxLatencyWindow {
		pm.recentLatencies = pm.recentLatencies[1:]
	}

	// Track request in minute bucket
	now := time.Now()
	minute := now.Unix() / 60
	idx := minute % dayInMinutes

	if pm.bucketTimes[idx] != minute {
		pm.buckets[idx] = 0
		pm.bucketTimes[idx] = minute
	}
	pm.buckets[idx]++
}

// RecordThrottle records a rate limiting or blocking response.
func (pm *ProviderMonitor) RecordThrottle(statusCode int, retryAfter string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.lastThrottleTime = time.Now()

	if statusCode == 429 {
		pm.status429Count++
		pm.retryAfterDuration = 60 * time.Second
	}

	if statusCode == 403 {
		pm.status403Count++
		pm.retryAfterDuration = 10 * time.Minute // Longer for IP block
	}
}

// DetectThrottlePattern checks if a message contains throttle patterns.
func (pm *ProviderMonitor) DetectThrottlePattern(message string) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	lowerMsg := strings.ToLower(message)

	for _, pattern := range pm.throttlePatterns {
		if strings.Contains(lowerMsg, pattern) {
			return true
		}
	}

	return false
}

// CheckProviderStatus returns the current status of the provider.
func (pm *ProviderMonitor) CheckProviderStatus() ProviderStatus {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// Blocked by 403
	if pm.status403Count > 0 && time.Since(pm.lastThrottleTime) < pm.retryAfterDuration {
		return StatusBlocked
	}

	// Throttled by 429
	if pm.status429Count > 5 && time.Since(pm.lastThrottleTime) < pm.retryAfterDuration {
		return StatusThrottled
	}

	// Check average latency
	if len(pm.recentLatencies) > 10 {
		var total time.Duration
		for _, lat := range pm.recentLatencies {
			total += lat
		}
		avg := total / time.Duration(len(pm.recentLatencies))

		if avg > pm.slowResponseThreshold {
			return StatusDegraded
		}
	}

	// Check sliding window usage (Last 24h)
	// Get total requests in last 24h
	totalReqs := 0
	nowMinute := time.Now().Unix() / 60
	cutoff := nowMinute - (24 * 60)

	for i := range dayInMinutes {
		if pm.bucketTimes[i] > cutoff {
			totalReqs += pm.buckets[i]
		}
	}

	usagePercent := float64(totalReqs) / float64(pm.EstimatedDailyLimit)
	if usagePercent > 0.9 {
		return StatusThrottled
	}

	return StatusHealthy
}

// GetRetryAfter returns remaining time before retry is allowed.
func (pm *ProviderMonitor) GetRetryAfter() time.Duration {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if pm.retryAfterDuration > 0 {
		remaining := pm.retryAfterDuration - time.Since(pm.lastThrottleTime)
		if remaining > 0 {
			return remaining
		}
	}

	return 0
}

// GetAverageLatency returns the average latency of recent requests.
func (pm *ProviderMonitor) GetAverageLatency() time.Duration {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if len(pm.recentLatencies) == 0 {
		return 0
	}

	var total time.Duration
	for _, lat := range pm.recentLatencies {
		total += lat
	}

	return total / time.Duration(len(pm.recentLatencies))
}

// GetRequestCount returns number of requests in the given duration.
// Approximation using minute buckets.
func (pm *ProviderMonitor) GetRequestCount(duration time.Duration) int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	minutes := int64(duration.Minutes())
	if minutes <= 0 {
		return 0
	}

	nowMinute := time.Now().Unix() / 60
	cutoff := nowMinute - minutes

	count := 0
	for i := range dayInMinutes {
		if pm.bucketTimes[i] > cutoff {
			count += pm.buckets[i]
		}
	}

	return count
}

// GetStats returns current monitoring statistics.
func (pm *ProviderMonitor) GetStats() MonitorStats {
	// Note: Calling exported methods that also lock, so we need unlocked versions or careful lock management.
	// We will implement logic inline to avoid double locking since we need to lock for consistency.

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// Recalculate usage
	nowMinute := time.Now().Unix() / 60

	// Last 1h
	reqLast1Hour := 0
	cutoff1h := nowMinute - 60
	for i := range dayInMinutes {
		if pm.bucketTimes[i] > cutoff1h {
			reqLast1Hour += pm.buckets[i]
		}
	}

	// Last 24h
	reqLast24Hours := 0
	cutoff24h := nowMinute - (24 * 60)
	for i := range dayInMinutes {
		if pm.bucketTimes[i] > cutoff24h {
			reqLast24Hours += pm.buckets[i]
		}
	}

	avgLatency := time.Duration(0)
	if len(pm.recentLatencies) > 0 {
		var total time.Duration
		for _, lat := range pm.recentLatencies {
			total += lat
		}
		avgLatency = total / time.Duration(len(pm.recentLatencies))
	}

	// Determine status inline (logic reused from CheckProviderStatus but without locking)
	status := StatusHealthy
	if pm.status403Count > 0 && time.Since(pm.lastThrottleTime) < pm.retryAfterDuration {
		status = StatusBlocked
	} else if pm.status429Count > 5 && time.Since(pm.lastThrottleTime) < pm.retryAfterDuration {
		status = StatusThrottled
	} else if avgLatency > pm.slowResponseThreshold {
		status = StatusDegraded
	} else {
		usagePercent := float64(reqLast24Hours) / float64(pm.EstimatedDailyLimit)
		if usagePercent > 0.9 {
			status = StatusThrottled
		}
	}

	stats := MonitorStats{
		Status:              status,
		AverageLatency:      avgLatency,
		ThrottleCount429:    pm.status429Count,
		ThrottleCount403:    pm.status403Count,
		RequestsLast1Hour:   reqLast1Hour,
		RequestsLast24Hours: reqLast24Hours,
		EstimatedDailyLimit: pm.EstimatedDailyLimit,
	}

	if reqLast24Hours > 0 {
		stats.UsagePercentage = float64(reqLast24Hours) / float64(pm.EstimatedDailyLimit) * 100
	}

	return stats
}

// SetDailyLimit updates the estimated daily limit.
func (pm *ProviderMonitor) SetDailyLimit(limit int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.EstimatedDailyLimit = limit
}

// GetHealthScore returns a normalized health score (0-100) for the provider.
// This consolidates status, latency, usage, and error counts into a single metric.
func (pm *ProviderMonitor) GetHealthScore() float64 {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	score := 100.0

	// Status penalty
	status := pm.checkStatusUnlocked()
	switch status {
	case StatusBlocked:
		return 0
	case StatusThrottled:
		score -= 60
	case StatusDegraded:
		score -= 30
	}

	// Latency penalty
	avgLatency := pm.getAvgLatencyUnlocked()
	if avgLatency > 3*time.Second {
		score -= 30
	} else if avgLatency > 2*time.Second {
		score -= 20
	} else if avgLatency > 1*time.Second {
		score -= 10
	}

	// Usage penalty
	usagePercent := pm.getUsagePercentUnlocked()
	if usagePercent > 90 {
		score -= 30
	} else if usagePercent > 75 {
		score -= 15
	} else if usagePercent > 50 {
		score -= 5
	}

	// Error penalties
	score -= float64(pm.status429Count) * 3
	score -= float64(pm.status403Count) * 8

	if score < 0 {
		score = 0
	}

	return score
}

// checkStatusUnlocked returns status without locking (caller must hold lock).
func (pm *ProviderMonitor) checkStatusUnlocked() ProviderStatus {
	if pm.status403Count > 0 && time.Since(pm.lastThrottleTime) < pm.retryAfterDuration {
		return StatusBlocked
	}
	if pm.status429Count > 5 && time.Since(pm.lastThrottleTime) < pm.retryAfterDuration {
		return StatusThrottled
	}
	avgLatency := pm.getAvgLatencyUnlocked()
	if avgLatency > pm.slowResponseThreshold {
		return StatusDegraded
	}
	usagePercent := pm.getUsagePercentUnlocked()
	if usagePercent > 90 {
		return StatusThrottled
	}
	return StatusHealthy
}

// getAvgLatencyUnlocked returns average latency without locking.
func (pm *ProviderMonitor) getAvgLatencyUnlocked() time.Duration {
	if len(pm.recentLatencies) == 0 {
		return 0
	}
	var total time.Duration
	for _, lat := range pm.recentLatencies {
		total += lat
	}
	return total / time.Duration(len(pm.recentLatencies))
}

// getUsagePercentUnlocked returns usage percentage without locking.
func (pm *ProviderMonitor) getUsagePercentUnlocked() float64 {
	nowMinute := time.Now().Unix() / 60
	cutoff := nowMinute - (24 * 60)
	totalReqs := 0
	for i := range dayInMinutes {
		if pm.bucketTimes[i] > cutoff {
			totalReqs += pm.buckets[i]
		}
	}
	return float64(totalReqs) / float64(pm.EstimatedDailyLimit) * 100
}
