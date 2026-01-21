package routing

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/rpc/provider"
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

// ErrorAction determines how to handle an error.
type ErrorAction int

const (
	ActionRetry ErrorAction = iota
	ActionFailover
	ActionFatal
)

// ClassifyError determines the action for a given error.
func ClassifyError(err error) ErrorAction {
	if err == nil {
		return ActionRetry // Should not happen
	}

	s := err.Error()
	sLower := strings.ToLower(s)

	// Fatal (Code or Request issues)
	// -32700: Parse error, -32600: Invalid Request, -32601: Method not found, -32602: Invalid params
	if strings.Contains(s, "-32700") || strings.Contains(s, "-32600") ||
		strings.Contains(s, "-32601") || strings.Contains(s, "-32602") {
		return ActionFatal
	}

	// Failover (Provider specific issues)
	if strings.Contains(s, "429") || strings.Contains(sLower, "too many requests") ||
		strings.Contains(s, "403") || strings.Contains(sLower, "forbidden") ||
		strings.Contains(sLower, "quota") || strings.Contains(sLower, "plan limit") ||
		strings.Contains(sLower, "unauthorized") ||
		strings.Contains(sLower, "rate limit") ||
		strings.Contains(sLower, "count exceeded") {
		return ActionFailover
	}

	// Default to Retry (Network, 5xx, etc)
	return ActionRetry
}

// CallWithRetry executes an RPC call with exponential backoff.
func CallWithRetry(
	ctx context.Context,
	p provider.RPCProvider,
	method string,
	params []any,
	config RetryConfig,
) (any, error) {
	var lastErr error

	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		result, err := p.Call(ctx, method, params)
		if err == nil {
			return result, nil
		}

		lastErr = err

		// Classify error
		action := ClassifyError(err)
		if action == ActionFatal {
			return nil, err // Stop immediately, do not retry
		}
		if action == ActionFailover {
			return nil, err // Return error immediately to try next provider
		}

		// ActionRetry: continue loop
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
func CallWithRetryAndFailover(
	ctx context.Context,
	router Router,
	chainID domain.ChainID,
	method string,
	params []any,
	config RetryConfig,
) (any, error) {
	providers := router.GetAllProviders(chainID)
	if len(providers) == 0 {
		return nil, fmt.Errorf("no providers for chain %s", chainID)
	}

	var lastErr error
	for _, p := range providers {
		rpcP, ok := p.(provider.RPCProvider)
		if !ok {
			continue
		}
		start := time.Now()
		result, err := CallWithRetry(ctx, rpcP, method, params, config)
		latency := time.Since(start)
		if err == nil {
			router.RecordSuccess(p.GetName(), latency)
			return result, nil
		}

		lastErr = err
		router.RecordFailure(p.GetName(), err)

		// Check if Fatal
		if ClassifyError(err) == ActionFatal {
			return nil, fmt.Errorf("fatal error from provider %s: %w", p.GetName(), err)
		}
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
