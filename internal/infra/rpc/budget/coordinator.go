package budget

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/vietddude/watcher/internal/infra/rpc/provider"
	"github.com/vietddude/watcher/internal/infra/rpc/routing"
)

// Coordinator unifies Budget and Router for coordinated provider decisions.
type Coordinator struct {
	mu sync.RWMutex

	router    routing.Router
	budget    BudgetTracker
	predictor *QuotaPredictor

	proactiveRotation   bool
	rotationThreshold   float64
	predictionThreshold time.Duration
	minRotationInterval time.Duration
	lastRotationTime    map[string]time.Time

	onRotation func(chainID, fromProvider, toProvider string, reason string)
}

// CoordinatorConfig holds configuration for the Coordinator.
type CoordinatorConfig struct {
	ProactiveRotation   bool
	RotationThreshold   float64
	PredictionThreshold time.Duration
	MinRotationInterval time.Duration
}

// DefaultCoordinatorConfig returns sensible defaults.
func DefaultCoordinatorConfig() CoordinatorConfig {
	return CoordinatorConfig{
		ProactiveRotation:   true,
		RotationThreshold:   75.0,
		PredictionThreshold: 15 * time.Minute,
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
		predictor:           NewQuotaPredictor(),
		proactiveRotation:   config.ProactiveRotation,
		rotationThreshold:   config.RotationThreshold,
		predictionThreshold: config.PredictionThreshold,
		minRotationInterval: config.MinRotationInterval,
		lastRotationTime:    make(map[string]time.Time),
	}
}

// GetBestProvider returns the best available provider.
func (c *Coordinator) GetBestProvider(chainID string) (provider.Provider, error) {
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

func (c *Coordinator) scoreProvider(chainID string, p provider.Provider) (float64, string) {
	score := 100.0
	var reasons []string

	usage := c.budget.GetProviderUsage(chainID, p.GetName())

	if usage.UsagePercentage >= 95 {
		return 0, "quota exhausted"
	} else if usage.UsagePercentage >= c.rotationThreshold {
		score -= 50
		reasons = append(reasons, fmt.Sprintf("high usage %.1f%%", usage.UsagePercentage))
	} else if usage.UsagePercentage >= 50 {
		score -= 20
	}

	if c.proactiveRotation {
		timeToExhaust := c.predictor.PredictTimeToExhaustion(p.GetName(), usage.RemainingCalls)
		if timeToExhaust > 0 && timeToExhaust < c.predictionThreshold {
			score -= 40
			reasons = append(
				reasons,
				fmt.Sprintf("predicted exhaustion in %v", timeToExhaust.Round(time.Minute)),
			)
		}
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
func (c *Coordinator) ShouldRotate(chainID, providerName string) (bool, string) {
	c.mu.RLock()
	lastRotation := c.lastRotationTime[chainID]
	c.mu.RUnlock()

	if time.Since(lastRotation) < c.minRotationInterval {
		return false, "too soon since last rotation"
	}

	usage := c.budget.GetProviderUsage(chainID, providerName)

	if usage.UsagePercentage >= c.rotationThreshold {
		return true, fmt.Sprintf("usage %.1f%% exceeds threshold %.1f%%",
			usage.UsagePercentage, c.rotationThreshold)
	}

	if c.proactiveRotation {
		timeToExhaust := c.predictor.PredictTimeToExhaustion(providerName, usage.RemainingCalls)
		if timeToExhaust > 0 && timeToExhaust < c.predictionThreshold {
			return true, fmt.Sprintf("predicted exhaustion in %v", timeToExhaust.Round(time.Minute))
		}
	}

	return false, ""
}

// RecordRequest records a request for rate tracking.
func (c *Coordinator) RecordRequest(chainID, providerName, method string) {
	c.predictor.RecordRequest(providerName)
	c.budget.RecordCall(chainID, providerName, method)
}

// RotateIfNeeded checks if rotation is needed and performs it.
func (c *Coordinator) RotateIfNeeded(
	chainID string,
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
	fn func(chainID, fromProvider, toProvider string, reason string),
) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onRotation = fn
}

// GetPredictionStats returns prediction statistics for a provider.
func (c *Coordinator) GetPredictionStats(chainID, providerName string) PredictionStats {
	usage := c.budget.GetProviderUsage(chainID, providerName)
	return c.predictor.GetPredictionStats(providerName, usage.RemainingCalls)
}

// CallWithCoordination makes a call with full coordination.
func (c *Coordinator) CallWithCoordination(
	ctx context.Context,
	chainID, method string,
	params []any,
) (any, error) {
	p, err := c.GetBestProvider(chainID)
	if err != nil {
		return nil, err
	}

	if delay := c.budget.GetThrottleDelay(chainID); delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	result, err := p.Call(ctx, method, params)

	c.RecordRequest(chainID, p.GetName(), method)

	if err == nil {
		c.router.RecordSuccess(p.GetName(), 0)
	} else {
		c.router.RecordFailure(p.GetName(), err)
		if _, rotated, _ := c.RotateIfNeeded(chainID, p); rotated {
			// Rotation happened
		}
	}

	return result, err
}

// GetRouter returns the underlying router.
func (c *Coordinator) GetRouter() routing.Router {
	return c.router
}

// GetBudget returns the underlying budget tracker.
func (c *Coordinator) GetBudget() BudgetTracker {
	return c.budget
}

// GetPredictor returns the quota predictor.
func (c *Coordinator) GetPredictor() *QuotaPredictor {
	return c.predictor
}
