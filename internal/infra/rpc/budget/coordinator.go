package budget

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/indexing/metrics"
	"github.com/vietddude/watcher/internal/infra/rpc/provider"
	"github.com/vietddude/watcher/internal/infra/rpc/routing"
)

// Coordinator unifies Budget and Router for coordinated provider decisions.
type Coordinator struct {
	mu sync.RWMutex

	router routing.Router
	budget BudgetTracker

	proactiveRotation   bool
	rotationThreshold   float64
	minRotationInterval time.Duration
	lastRotationTime    map[domain.ChainID]time.Time

	onRotation func(chainID domain.ChainID, fromProvider, toProvider string, reason string)
}

// CoordinatorConfig holds configuration for the Coordinator.
type CoordinatorConfig struct {
	ProactiveRotation   bool
	RotationThreshold   float64
	MinRotationInterval time.Duration
}

// DefaultCoordinatorConfig returns sensible defaults.
func DefaultCoordinatorConfig() CoordinatorConfig {
	return CoordinatorConfig{
		ProactiveRotation:   true,
		RotationThreshold:   75.0,
		MinRotationInterval: 2 * time.Minute,
	}
}

// NewCoordinator creates a new coordinator with default config.
func NewCoordinator(router routing.Router, budget BudgetTracker) *Coordinator {
	return NewCoordinatorWithConfig(router, budget, DefaultCoordinatorConfig())
}

// NewCoordinatorWithConfig creates a coordinator with custom config.
func NewCoordinatorWithConfig(
	router routing.Router,
	budget BudgetTracker,
	config CoordinatorConfig,
) *Coordinator {
	return &Coordinator{
		router:              router,
		budget:              budget,
		proactiveRotation:   config.ProactiveRotation,
		rotationThreshold:   config.RotationThreshold,
		minRotationInterval: config.MinRotationInterval,
		lastRotationTime:    make(map[domain.ChainID]time.Time),
	}
}

// GetBestProvider returns the best available provider.
func (c *Coordinator) GetBestProvider(chainID domain.ChainID) (provider.Provider, error) {
	providers := c.router.GetAllProviders(chainID)
	if len(providers) == 0 {
		return nil, fmt.Errorf("no providers for chain %s", chainID)
	}

	type scoredProvider struct {
		provider provider.Provider
		score    float64
		reason   string
	}

	var scored []scoredProvider

	for _, p := range providers {
		score, reason := c.scoreProvider(chainID, p)
		if score > 0 {
			scored = append(scored, scoredProvider{
				provider: p,
				score:    score,
				reason:   reason,
			})
		}
	}

	if len(scored) == 0 {
		return c.router.GetProvider(chainID)
	}

	best := scored[0]
	for _, sp := range scored[1:] {
		if sp.score > best.score {
			best = sp
		}
	}

	return best.provider, nil
}

func (c *Coordinator) scoreProvider(_ domain.ChainID, p provider.Provider) (float64, string) {
	score := 100.0
	var reasons []string

	usage := c.budget.GetProviderUsage(p.GetName())

	if usage.UsagePercentage >= 95 {
		return 0, "quota exhausted"
	} else if usage.UsagePercentage >= c.rotationThreshold {
		score -= 50
		reasons = append(reasons, fmt.Sprintf("high usage %.1f%%", usage.UsagePercentage))
	} else if usage.UsagePercentage >= 50 {
		score -= 20
	}

	if httpProv, ok := p.(*provider.HTTPProvider); ok {
		stats := httpProv.Monitor.GetStats()

		switch stats.Status {
		case provider.StatusBlocked:
			return 0, "blocked"
		case provider.StatusThrottled:
			score -= 60
			reasons = append(reasons, "throttled")
		case provider.StatusDegraded:
			score -= 30
			reasons = append(reasons, "degraded")
		}

		if stats.AverageLatency > 2*time.Second {
			score -= 20
		}

		score -= float64(stats.ThrottleCount429) * 5
		score -= float64(stats.ThrottleCount403) * 10
	}

	reason := "healthy"
	if len(reasons) > 0 {
		reason = reasons[0]
	}

	return max(0, score), reason
}

// ShouldRotate checks if rotation is advisable.
func (c *Coordinator) ShouldRotate(chainID domain.ChainID, providerName string) (bool, string) {
	c.mu.RLock()
	lastRotation := c.lastRotationTime[chainID]
	c.mu.RUnlock()

	if time.Since(lastRotation) < c.minRotationInterval {
		return false, "too soon since last rotation"
	}

	usage := c.budget.GetProviderUsage(providerName)

	if usage.UsagePercentage >= c.rotationThreshold {
		return true, fmt.Sprintf("usage %.1f%% exceeds threshold %.1f%%",
			usage.UsagePercentage, c.rotationThreshold)
	}

	return false, ""
}

// RecordRequest records a request for rate tracking.
func (c *Coordinator) RecordRequest(providerName, method string) {
	c.budget.RecordCall(providerName, method)
}

// RotateIfNeeded checks if rotation is needed and performs it.
func (c *Coordinator) RotateIfNeeded(
	chainID domain.ChainID,
	currentProvider provider.Provider,
) (provider.Provider, bool, string) {
	shouldRotate, reason := c.ShouldRotate(chainID, currentProvider.GetName())
	if !shouldRotate {
		return currentProvider, false, ""
	}

	newProvider, err := c.GetBestProvider(chainID)
	if err != nil || newProvider.GetName() == currentProvider.GetName() {
		return currentProvider, false, "no better provider available"
	}

	c.mu.Lock()
	c.lastRotationTime[chainID] = time.Now()
	c.mu.Unlock()

	if c.onRotation != nil {
		c.onRotation(chainID, currentProvider.GetName(), newProvider.GetName(), reason)
	}

	return newProvider, true, reason
}

// SetRotationCallback sets a callback for rotation events.
func (c *Coordinator) SetRotationCallback(
	fn func(chainID domain.ChainID, fromProvider, toProvider string, reason string),
) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onRotation = fn
}

// ForceRotate forces a rotation to the next best provider, ignoring cooldowns.
// This is useful for immediate failover.
func (c *Coordinator) ForceRotate(
	chainID domain.ChainID,
	currentProviderName string,
) (provider.Provider, error) {
	c.mu.Lock()
	// Reset rotation timer to allow immediate rotation
	c.lastRotationTime[chainID] = time.Time{}
	c.mu.Unlock()

	newProvider, err := c.GetBestProvider(chainID)
	if err != nil {
		return nil, err
	}

	if newProvider.GetName() == currentProviderName {
		// If BestProvider is still the same, we force the router to rotate.
		// Since RotateProvider is random, we try a few times to get a different one.
		for i := 0; i < 5; i++ {
			p, err := c.router.RotateProvider(chainID)
			if err != nil {
				return nil, err
			}
			newProvider = p
			if newProvider.GetName() != currentProviderName {
				break
			}
		}
	}

	c.mu.Lock()
	c.lastRotationTime[chainID] = time.Now()
	c.mu.Unlock()

	if c.onRotation != nil {
		c.onRotation(chainID, currentProviderName, newProvider.GetName(), "forced failover")
	}

	return newProvider, nil
}

// Call executes an RPC call with coordinated budget checking, monitoring, and smart failover.
// This is the primary entry point for the Client.
func (c *Coordinator) Call(
	ctx context.Context,
	chainID domain.ChainID, method string,
	params []any,
) (any, error) {
	// 1. Budget Throttling - check current provider
	p, err := c.GetBestProvider(chainID)
	if err != nil {
		return nil, err
	}
	if !c.budget.CanMakeCall(p.GetName()) {
		if delay := c.budget.GetThrottleDelay(p.GetName()); delay > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
	}

	// 3. Execution Loop (Retry & Failover)
	// We use the RetryConfig from routing, but we manage the high-level failover here.
	const MaxFailoverAttempts = 3
	var lastErr error

	for attempt := 0; attempt < MaxFailoverAttempts; attempt++ {
		// Ensure provider supports RPC
		rpcP, ok := p.(provider.RPCProvider)
		if !ok {
			lastErr = fmt.Errorf("provider %s does not support RPC calls", p.GetName())
			// Try next one immediately
			p, err = c.ForceRotate(chainID, p.GetName())
			if err != nil {
				return nil, err
			}
			continue
		}

		// Execute with internal retry (handling transient network errors)
		start := time.Now()
		result, err := routing.CallWithRetry(ctx, rpcP, method, params, routing.DefaultRetryConfig)
		latency := time.Since(start)

		c.RecordRequest(p.GetName(), method)

		if err == nil {
			c.router.RecordSuccess(p.GetName(), latency) // Fix validation: passing actual latency
			return result, nil
		}

		// Handle Failure
		c.router.RecordFailure(p.GetName(), err)
		lastErr = err

		// Classify Error
		action := routing.ClassifyError(err)

		if action == routing.ActionFatal {
			return nil, err // Stop immediately
		}

		// If Failover or Retry (exhausted), we rotate
		if attempt < MaxFailoverAttempts-1 {
			// Rotate to next provider
			nextP, rotErr := c.ForceRotate(chainID, p.GetName())
			if rotErr != nil {
				// No other provider available or rotation failed
				break
			}
			p = nextP
		}
	}

	return nil, fmt.Errorf("call %s failed after %d failovers: %w", method, MaxFailoverAttempts, lastErr)
}

// Execute performs an operation with coordinated budget checking and failover.
// This is the new entry point for Operation-based calls (supporting both HTTP and gRPC).
func (c *Coordinator) Execute(
	ctx context.Context,
	chainID domain.ChainID,
	op provider.Operation,
) (any, error) {
	// 1. Budget Throttling - check current provider
	p, err := c.GetBestProvider(chainID)
	if err != nil {
		return nil, err
	}
	if !c.budget.CanMakeCall(p.GetName()) {
		if delay := c.budget.GetThrottleDelay(p.GetName()); delay > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
	}

	// 3. Execution Loop (Failover only)
	// Providers are responsible for their own internal retries (e.g., GRPCProvider).
	// We handles rotating to a different provider if the current one fails completely.
	const MaxFailoverAttempts = 3
	var lastErr error

	for attempt := 0; attempt < MaxFailoverAttempts; attempt++ {
		// Execute on provider
		result, err := p.Execute(ctx, op)

		if err == nil {
			// Only record usage on success
			c.RecordRequest(p.GetName(), op.Name)
			return result, nil
		}

		// Handle Failure
		c.router.RecordFailure(p.GetName(), err)
		lastErr = err

		// Check if we should failover
		// Optimization: Do NOT failover immediately for "Not Found" errors
		// These are expected when polling the tip
		if routing.ClassifyError(err) == routing.ActionFatal {
			return nil, err
		}

		isNotFound := false
		if strings.Contains(strings.ToLower(err.Error()), "not found") ||
			strings.Contains(strings.ToLower(err.Error()), "out of range") {
			isNotFound = true
		}

		if isNotFound {
			return nil, err // Return immediately to avoid failover burst
		}
		if attempt < MaxFailoverAttempts-1 {
			// Rotate to next provider
			nextP, rotErr := c.ForceRotate(chainID, p.GetName())
			if rotErr != nil {
				break
			}
			p = nextP
		}
	}

	return nil, fmt.Errorf("execute %s failed after %d failovers: %w", op.Name, MaxFailoverAttempts, lastErr)
}

// CallWithCoordination is deprecated. Use Call instead.
func (c *Coordinator) CallWithCoordination(
	ctx context.Context,
	chainID domain.ChainID, method string,
	params []any,
) (any, error) {
	return c.Call(ctx, chainID, method, params)
}

// GetRouter returns the underlying router.
func (c *Coordinator) GetRouter() routing.Router {
	return c.router
}

// GetBudget returns the underlying budget tracker.
func (c *Coordinator) GetBudget() BudgetTracker {
	return c.budget
}

// UpdateMetrics updates Prometheus metrics for all providers.
// Call this periodically (e.g., every 10 seconds) from a background goroutine.
func (c *Coordinator) UpdateMetrics(chainID domain.ChainID) {
	providers := c.router.GetAllProviders(chainID)
	chainName, _ := domain.ChainNameFromID(chainID)
	for _, p := range providers {
		// 1. Update Budget Metrics (Universal)
		// This comes from the budget tracker and is available for all providers (HTTP, GRPC, etc.)
		budgetStats := c.budget.GetProviderUsage(p.GetName())
		metrics.RPCQuotaRemaining.WithLabelValues(chainName, p.GetName()).
			Set(float64(budgetStats.RemainingCalls))

		// 2. Update Provider-Specific Metrics
		// Try to get detailed stats from HTTPProvider first
		httpProv, ok := p.(*provider.HTTPProvider)
		if ok {
			stats := httpProv.Monitor.GetStats()
			healthScore := httpProv.Monitor.GetHealthScore()

			metrics.RPCProviderHealthScore.WithLabelValues(chainName, p.GetName()).Set(healthScore)
			metrics.RPCProviderQuotaUsage.WithLabelValues(chainName, p.GetName()).
				Set(stats.UsagePercentage / 100.0)
			metrics.RPCProviderLatencySeconds.WithLabelValues(chainName, p.GetName()).
				Set(stats.AverageLatency.Seconds())
			continue
		}

		// Fallback for generic providers (e.g. GRPC)
		health := p.GetHealth()
		// HealthScore is not directly available on generic interface, default to 100 if available
		healthScore := 0.0
		if health.Available {
			healthScore = 100.0 // Simplified assumption
		}
		metrics.RPCProviderHealthScore.WithLabelValues(chainName, p.GetName()).Set(healthScore)

		if health.MonitorStats != nil {
			metrics.RPCProviderQuotaUsage.WithLabelValues(chainName, p.GetName()).
				Set(health.MonitorStats.UsagePercentage / 100.0)
			metrics.RPCProviderLatencySeconds.WithLabelValues(chainName, p.GetName()).
				Set(health.MonitorStats.AverageLatency.Seconds())
		}
	}
}
