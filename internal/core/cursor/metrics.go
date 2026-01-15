package cursor

import (
	"time"
)

// blockRecord holds timing data for a processed block.
type blockRecord struct {
	BlockNumber uint64
	ProcessedAt time.Time
}

// Metrics holds cursor performance data.
type Metrics struct {
	BlocksPerSecond   float64
	AverageBlockTime  time.Duration
	LastReorgAt       *time.Time
	MissingBlockCount int
	StateHistory      []Transition
}

// MetricsCollector tracks cursor performance over time.
type MetricsCollector struct {
	windowSize  int           // number of blocks to track
	blockTimes  []blockRecord // ring buffer of block records
	transitions []Transition  // recent state changes
	lastReorgAt *time.Time
}

// RecordBlock records timing for a processed block.
func (mc *MetricsCollector) RecordBlock(blockNumber uint64, processedAt time.Time) {
	record := blockRecord{
		BlockNumber: blockNumber,
		ProcessedAt: processedAt,
	}

	if len(mc.blockTimes) >= mc.windowSize {
		// Shift elements left, drop oldest
		copy(mc.blockTimes, mc.blockTimes[1:])
		mc.blockTimes[len(mc.blockTimes)-1] = record
	} else {
		mc.blockTimes = append(mc.blockTimes, record)
	}
}

// RecordTransition records a state transition.
func (mc *MetricsCollector) RecordTransition(t Transition) {
	// Keep only last 10 transitions
	if len(mc.transitions) >= 10 {
		copy(mc.transitions, mc.transitions[1:])
		mc.transitions[len(mc.transitions)-1] = t
	} else {
		mc.transitions = append(mc.transitions, t)
	}

	// Track last reorg
	if t.To == StateReorg {
		now := t.Timestamp
		mc.lastReorgAt = &now
	}
}

// GetMetrics returns current metrics.
func (mc *MetricsCollector) GetMetrics() Metrics {
	m := Metrics{
		LastReorgAt:  mc.lastReorgAt,
		StateHistory: make([]Transition, len(mc.transitions)),
	}
	copy(m.StateHistory, mc.transitions)

	// Calculate blocks per second
	if len(mc.blockTimes) >= 2 {
		first := mc.blockTimes[0]
		last := mc.blockTimes[len(mc.blockTimes)-1]
		duration := last.ProcessedAt.Sub(first.ProcessedAt)

		if duration > 0 {
			blockCount := float64(len(mc.blockTimes) - 1)
			m.BlocksPerSecond = blockCount / duration.Seconds()
			m.AverageBlockTime = time.Duration(float64(duration) / blockCount)
		}
	}

	return m
}

// Reset clears all collected metrics.
func (mc *MetricsCollector) Reset() {
	mc.blockTimes = mc.blockTimes[:0]
	mc.transitions = mc.transitions[:0]
	mc.lastReorgAt = nil
}
