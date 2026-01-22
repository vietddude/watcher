package throttle

import (
	"testing"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
)

func TestComputeInterval(t *testing.T) {
	config := DefaultConfig()
	config.MinScanInterval = 500 * time.Millisecond
	config.MaxScanInterval = 60 * time.Second
	config.LagNormalThreshold = 5
	config.LagBurstThreshold = 50

	baseScanInterval := 12 * time.Second
	controller := NewAdaptiveController(domain.EthereumMainnet, baseScanInterval, config)

	tests := []struct {
		name     string
		lag      int64
		expected time.Duration
	}{
		{
			name:     "at chain head (lag=0)",
			lag:      0,
			expected: 12 * time.Second, // base interval
		},
		{
			name:     "slightly behind (lag=3)",
			lag:      3,
			expected: 6 * time.Second, // base / 2
		},
		{
			name:     "catching up (lag=20)",
			lag:      20,
			expected: 1 * time.Second, // min * 2
		},
		{
			name:     "far behind (lag=100)",
			lag:      100,
			expected: 500 * time.Millisecond, // min interval
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := controller.ComputeInterval(tt.lag)
			if result != tt.expected {
				t.Errorf("ComputeInterval(%d) = %v, want %v", tt.lag, result, tt.expected)
			}
		})
	}
}

func TestComputeBatchSize(t *testing.T) {
	config := DefaultConfig()
	config.MinBatchSize = 1
	config.MaxBatchSize = 20
	config.HighLatencyThreshold = 2 * time.Second

	controller := NewAdaptiveController(domain.EthereumMainnet, 12*time.Second, config)

	tests := []struct {
		name       string
		lag        int64
		latency    time.Duration
		expected   int
		shouldClip bool
	}{
		{
			name:     "at chain head",
			lag:      0,
			latency:  500 * time.Millisecond,
			expected: 1,
		},
		{
			name:     "slightly behind",
			lag:      5,
			latency:  500 * time.Millisecond,
			expected: 3,
		},
		{
			name:     "catching up",
			lag:      50,
			latency:  500 * time.Millisecond,
			expected: 10,
		},
		{
			name:     "far behind",
			lag:      200,
			latency:  500 * time.Millisecond,
			expected: 20, // max batch
		},
		{
			name:     "high latency - conservative batch",
			lag:      100,
			latency:  3 * time.Second,
			expected: 1, // min batch due to high latency
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := controller.ComputeBatchSize(tt.lag, tt.latency)
			if result != tt.expected {
				t.Errorf("ComputeBatchSize(lag=%d, latency=%v) = %d, want %d",
					tt.lag, tt.latency, result, tt.expected)
			}
		})
	}
}

func TestComputeInterval_Disabled(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = false

	baseScanInterval := 12 * time.Second
	controller := NewAdaptiveController(domain.EthereumMainnet, baseScanInterval, config)

	// When disabled, should always return base interval
	result := controller.ComputeInterval(100)
	if result != baseScanInterval {
		t.Errorf("ComputeInterval with disabled config = %v, want %v", result, baseScanInterval)
	}
}

func TestComputeBatchSize_Disabled(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = false

	controller := NewAdaptiveController(domain.EthereumMainnet, 12*time.Second, config)

	// When disabled, should always return 1
	result := controller.ComputeBatchSize(100, 500*time.Millisecond)
	if result != 1 {
		t.Errorf("ComputeBatchSize with disabled config = %d, want 1", result)
	}
}

func TestComputeBatchSize_BatchDisabled(t *testing.T) {
	config := DefaultConfig()
	config.BatchEnabled = false

	controller := NewAdaptiveController(domain.EthereumMainnet, 12*time.Second, config)

	// When batch disabled, should always return 1
	result := controller.ComputeBatchSize(100, 500*time.Millisecond)
	if result != 1 {
		t.Errorf("ComputeBatchSize with batch disabled = %d, want 1", result)
	}
}
