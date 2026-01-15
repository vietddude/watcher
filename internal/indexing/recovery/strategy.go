package recovery

import (
	"math"
	"time"
)

// RetryStrategy defines how retries should be handled.
type RetryStrategy interface {
	// GetDelay returns the delay for the given attempt (0-indexed).
	GetDelay(attempt int) time.Duration

	// ShouldRetry checks if we should retry based on the error and attempt count.
	ShouldRetry(err error, attempt int) bool
}

// ExponentialBackoff implements a standard backoff strategy.
type ExponentialBackoff struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	MaxAttempts  int
	Classifier   Classifier
}

// DefaultBackoff returns sensible defaults for blockchain indexing.
// 2s, 4s, 8s, 16s, 32s (Max 60s)
func DefaultBackoff(classifier Classifier) *ExponentialBackoff {
	if classifier == nil {
		// Default classifier treats everything as transient (safe default)
		classifier = func(err error) FailureCategory {
			return CategoryTransient
		}
	}
	return &ExponentialBackoff{
		InitialDelay: 2 * time.Second,
		MaxDelay:     60 * time.Second,
		MaxAttempts:  5,
		Classifier:   classifier,
	}
}

// GetDelay calculates delay: InitialDelay * 2^attempt
func (s *ExponentialBackoff) GetDelay(attempt int) time.Duration {
	delay := float64(s.InitialDelay) * math.Pow(2, float64(attempt))
	if delay > float64(s.MaxDelay) {
		return s.MaxDelay
	}
	return time.Duration(delay)
}

// ShouldRetry checks if error is transient and max attempts not exceeded.
func (s *ExponentialBackoff) ShouldRetry(err error, attempt int) bool {
	if attempt >= s.MaxAttempts {
		return false
	}

	category := s.Classifier(err)
	return category == CategoryTransient
}
