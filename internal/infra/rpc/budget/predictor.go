package budget

import (
	"sync"
	"time"
)

// PredictionStats holds prediction data for a provider.
type PredictionStats struct {
	RequestRatePerMin  float64
	WeightedRatePerMin float64
	TimeToExhaustion   time.Duration
	Trend              float64 // Positive = increasing rate
	RemainingQuota     int
}

// QuotaPredictor predicts when providers will exhaust their quota.
type QuotaPredictor struct {
	mu sync.RWMutex

	providerRates map[string]*RateTracker
	windowSize    time.Duration
	sampleCount   int
}

// RateTracker tracks request timing for rate calculation.
type RateTracker struct {
	timestamps  []time.Time
	ratesPerMin []float64
	maxSamples  int
	maxRates    int
}

// NewQuotaPredictor creates a new predictor with default settings.
func NewQuotaPredictor() *QuotaPredictor {
	return &QuotaPredictor{
		providerRates: make(map[string]*RateTracker),
		windowSize:    5 * time.Minute,
		sampleCount:   12,
	}
}

// RecordRequest records a request for rate tracking.
func (p *QuotaPredictor) RecordRequest(providerName string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	tracker := p.getOrCreateTracker(providerName)
	now := time.Now()

	tracker.timestamps = append(tracker.timestamps, now)

	// Prune old timestamps
	cutoff := now.Add(-p.windowSize)
	filtered := make([]time.Time, 0, len(tracker.timestamps))
	for _, ts := range tracker.timestamps {
		if ts.After(cutoff) {
			filtered = append(filtered, ts)
		}
	}
	tracker.timestamps = filtered

	if len(tracker.timestamps) > tracker.maxSamples {
		tracker.timestamps = tracker.timestamps[len(tracker.timestamps)-tracker.maxSamples:]
	}
}

// GetRequestRate returns the current request rate (requests per minute).
func (p *QuotaPredictor) GetRequestRate(providerName string) float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	tracker, ok := p.providerRates[providerName]
	if !ok || len(tracker.timestamps) < 2 {
		return 0
	}

	now := time.Now()
	cutoff := now.Add(-p.windowSize)

	var count int
	for _, ts := range tracker.timestamps {
		if ts.After(cutoff) {
			count++
		}
	}

	windowMins := p.windowSize.Minutes()
	if windowMins == 0 {
		return 0
	}

	return float64(count) / windowMins
}

// PredictTimeToExhaustion predicts how long until quota is exhausted.
func (p *QuotaPredictor) PredictTimeToExhaustion(providerName string, remainingQuota int) time.Duration {
	rate := p.GetRequestRate(providerName)

	if rate <= 0 || remainingQuota <= 0 {
		return 0
	}

	minutes := float64(remainingQuota) / rate
	return time.Duration(minutes) * time.Minute
}

// ShouldRotatePreemptively checks if rotation should happen before exhaustion.
func (p *QuotaPredictor) ShouldRotatePreemptively(providerName string, remainingQuota int, threshold time.Duration) bool {
	timeToExhaust := p.PredictTimeToExhaustion(providerName, remainingQuota)

	if timeToExhaust == 0 {
		return false
	}

	return timeToExhaust < threshold
}

// GetTrend returns the rate trend: positive = increasing, negative = decreasing.
func (p *QuotaPredictor) GetTrend(providerName string) float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	tracker, ok := p.providerRates[providerName]
	if !ok || len(tracker.ratesPerMin) < 2 {
		return 0
	}

	rates := tracker.ratesPerMin
	mid := len(rates) / 2

	var oldSum, newSum float64
	for i := 0; i < mid; i++ {
		oldSum += rates[i]
	}
	for i := mid; i < len(rates); i++ {
		newSum += rates[i]
	}

	oldAvg := oldSum / float64(mid)
	newAvg := newSum / float64(len(rates)-mid)

	if oldAvg == 0 {
		return 0
	}

	return (newAvg - oldAvg) / oldAvg
}

// GetPredictionStats returns prediction statistics for a provider.
func (p *QuotaPredictor) GetPredictionStats(providerName string, remainingQuota int) PredictionStats {
	return PredictionStats{
		RequestRatePerMin: p.GetRequestRate(providerName),
		TimeToExhaustion:  p.PredictTimeToExhaustion(providerName, remainingQuota),
		Trend:             p.GetTrend(providerName),
		RemainingQuota:    remainingQuota,
	}
}

// Reset clears all tracking data.
func (p *QuotaPredictor) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.providerRates = make(map[string]*RateTracker)
}

func (p *QuotaPredictor) getOrCreateTracker(providerName string) *RateTracker {
	tracker, ok := p.providerRates[providerName]
	if !ok {
		tracker = &RateTracker{
			timestamps:  make([]time.Time, 0, 1000),
			ratesPerMin: make([]float64, 0, 100),
			maxSamples:  1000,
			maxRates:    100,
		}
		p.providerRates[providerName] = tracker
	}
	return tracker
}
