package routing

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/vietddude/watcher/internal/rpc/provider"
)

// RetryConfig defines retry behavior.
type RetryConfig struct {
	MaxAttempts     int
	InitialDelay    time.Duration
	MaxDelay        time.Duration
	BackoffMultiple float64
}

// DefaultRetryConfig provides sensible defaults.
var DefaultRetryConfig = RetryConfig{
	MaxAttempts:     5,
	InitialDelay:    1 * time.Second,
	MaxDelay:        60 * time.Second,
	BackoffMultiple: 2.0,
}

// CallWithRetry executes an RPC call with exponential backoff.
func CallWithRetry(ctx context.Context, p provider.Provider, method string, params []any, config RetryConfig) (any, error) {
	var lastErr error

	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		result, err := p.Call(ctx, method, params)
		if err == nil {
			return result, nil
		}

		lastErr = err

		if attempt == config.MaxAttempts-1 {
			break
		}

		delay := calculateBackoff(attempt, config)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	return nil, fmt.Errorf("failed after %d attempts: %w", config.MaxAttempts, lastErr)
}

// CallWithRetryAndFailover tries multiple providers with retry.
func CallWithRetryAndFailover(ctx context.Context, router Router, chainID string, method string, params []any, config RetryConfig) (any, error) {
	providers := router.GetAllProviders(chainID)
	if len(providers) == 0 {
		return nil, fmt.Errorf("no providers for chain %s", chainID)
	}

	var lastErr error
	for _, p := range providers {
		result, err := CallWithRetry(ctx, p, method, params, config)
		if err == nil {
			router.RecordSuccess(p.GetName(), 0)
			return result, nil
		}

		lastErr = err
		router.RecordFailure(p.GetName(), err)
	}

	return nil, fmt.Errorf("all providers failed: %w", lastErr)
}

func calculateBackoff(attempt int, config RetryConfig) time.Duration {
	delay := float64(config.InitialDelay) * math.Pow(config.BackoffMultiple, float64(attempt))
	if delay > float64(config.MaxDelay) {
		delay = float64(config.MaxDelay)
	}
	return time.Duration(delay)
}
