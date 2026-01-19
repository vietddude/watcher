package provider

import (
	"strings"
	"sync"
	"time"
)

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

	// Sliding window
	requestTimestamps   []time.Time
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
		requestTimestamps:     make([]time.Time, 0),
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

	now := time.Now()

	// Track latency
	pm.recentLatencies = append(pm.recentLatencies, latency)
	if len(pm.recentLatencies) > pm.maxLatencyWindow {
		pm.recentLatencies = pm.recentLatencies[1:]
	}

	// Track timestamp for sliding window
	pm.requestTimestamps = append(pm.requestTimestamps, now)

	// Clean old timestamps outside window
	cutoff := now.Add(-pm.windowDuration)
	filtered := make([]time.Time, 0)
	for _, t := range pm.requestTimestamps {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	pm.requestTimestamps = filtered
}

// RecordThrottle records a rate limiting or blocking response.
func (pm *ProviderMonitor) RecordThrottle(statusCode int, retryAfter string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.lastThrottleTime = time.Now()

	if statusCode == 429 {
		pm.status429Count++
		if retryAfter != "" {
			pm.retryAfterDuration = 60 * time.Second // Default 1min
		}
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

	// Check sliding window usage
	usagePercent := float64(len(pm.requestTimestamps)) / float64(pm.EstimatedDailyLimit)
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
func (pm *ProviderMonitor) GetRequestCount(duration time.Duration) int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	cutoff := time.Now().Add(-duration)
	count := 0

	for _, t := range pm.requestTimestamps {
		if t.After(cutoff) {
			count++
		}
	}

	return count
}

// GetStats returns current monitoring statistics.
func (pm *ProviderMonitor) GetStats() MonitorStats {
	// Note: Calling exported methods that also lock, so we need unlocked versions
	status := pm.CheckProviderStatus()
	avgLatency := pm.GetAverageLatency()
	reqLast1Hour := pm.GetRequestCount(time.Hour)

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	stats := MonitorStats{
		Status:              status,
		AverageLatency:      avgLatency,
		ThrottleCount429:    pm.status429Count,
		ThrottleCount403:    pm.status403Count,
		RequestsLast1Hour:   reqLast1Hour,
		RequestsLast24Hours: len(pm.requestTimestamps),
		EstimatedDailyLimit: pm.EstimatedDailyLimit,
	}

	if len(pm.requestTimestamps) > 0 {
		stats.UsagePercentage = float64(
			len(pm.requestTimestamps),
		) / float64(
			pm.EstimatedDailyLimit,
		) * 100
	}

	return stats
}

// SetDailyLimit updates the estimated daily limit.
func (pm *ProviderMonitor) SetDailyLimit(limit int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.EstimatedDailyLimit = limit
}
