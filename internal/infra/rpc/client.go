package rpc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vietddude/watcher/internal/infra/rpc/budget"
	"github.com/vietddude/watcher/internal/infra/rpc/provider"
	"github.com/vietddude/watcher/internal/infra/rpc/routing"
)

// Client is the high-level interface for making RPC calls.
// This is what application layers should use.
type Client struct {
	router      routing.Router
	budget      budget.BudgetTracker
	chainID     string
	coordinator *budget.Coordinator

	autoRotateEnabled   bool
	rotateThreshold     float64
	lastRotationTime    time.Time
	minRotationInterval time.Duration

	predictiveRotation  bool
	predictionThreshold time.Duration
}

// NewClient creates a new RPC client.
func NewClient(chainID string, router routing.Router, b budget.BudgetTracker) *Client {
	return &Client{
		chainID:             chainID,
		router:              router,
		budget:              b,
		autoRotateEnabled:   true,
		rotateThreshold:     85.0,
		minRotationInterval: 5 * time.Minute,
		predictiveRotation:  true,
		predictionThreshold: 15 * time.Minute,
	}
}

// NewClientWithCoordinator creates a client with full coordinator support.
func NewClientWithCoordinator(chainID string, coordinator *budget.Coordinator) *Client {
	return &Client{
		chainID:             chainID,
		router:              coordinator.GetRouter(),
		budget:              coordinator.GetBudget(),
		coordinator:         coordinator,
		autoRotateEnabled:   true,
		rotateThreshold:     85.0,
		minRotationInterval: 2 * time.Minute,
		predictiveRotation:  true,
		predictionThreshold: 15 * time.Minute,
	}
}

// Call makes an RPC call with automatic failover and retry.
func (c *Client) Call(ctx context.Context, method string, params []any) (any, error) {
	if c.coordinator != nil {
		return c.coordinator.CallWithCoordination(ctx, c.chainID, method, params)
	}

	if c.autoRotateEnabled {
		c.checkAndRotate()
	}

	if !c.budget.CanMakeCall(c.chainID) {
		delay := c.budget.GetThrottleDelay(c.chainID)
		if delay > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
	}

	p, err := c.getQuotaAwareProvider()
	if err != nil {
		return nil, err
	}

	result, err := routing.CallWithRetry(ctx, p, method, params, routing.DefaultRetryConfig)

	if err == nil {
		c.router.RecordSuccess(p.GetName(), 0)
		c.budget.RecordCall(c.chainID, p.GetName(), method)
	} else {
		c.router.RecordFailure(p.GetName(), err)

		if c.shouldRetryWithRotation(err) {
			p, err = c.router.RotateProvider(c.chainID)
			if err == nil {
				result, err = routing.CallWithRetry(ctx, p, method, params, routing.DefaultRetryConfig)
				if err == nil {
					c.router.RecordSuccess(p.GetName(), 0)
					c.budget.RecordCall(c.chainID, p.GetName(), method)
				}
			}
		}
	}

	return result, err
}

// CallWithFailover tries all providers if needed.
func (c *Client) CallWithFailover(ctx context.Context, method string, params []any) (any, error) {
	if !c.budget.CanMakeCall(c.chainID) {
		delay := c.budget.GetThrottleDelay(c.chainID)
		if delay > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
	}

	result, err := routing.CallWithRetryAndFailover(
		ctx,
		c.router,
		c.chainID,
		method,
		params,
		routing.DefaultRetryConfig,
	)

	if err == nil {
		p, _ := c.router.GetProvider(c.chainID)
		if p != nil {
			c.budget.RecordCall(c.chainID, p.GetName(), method)
		}
	}

	return result, err
}

// ForceRotation forces a provider rotation.
func (c *Client) ForceRotation() error {
	_, err := c.router.RotateProvider(c.chainID)
	if err == nil {
		c.lastRotationTime = time.Now()
	}
	return err
}

// SetRotationStrategy updates the rotation strategy.
func (c *Client) SetRotationStrategy(strategy routing.RotationStrategy) {
	if defaultRouter, ok := c.router.(*routing.DefaultRouter); ok {
		defaultRouter.SetRotationStrategy(strategy)
	}
}

// GetUsage returns current budget usage.
func (c *Client) GetUsage() budget.UsageStats {
	return c.budget.GetUsage(c.chainID)
}

// GetProviderStats returns monitoring stats for all providers.
func (c *Client) GetProviderStats() map[string]provider.MonitorStats {
	providers := c.router.GetAllProviders(c.chainID)
	stats := make(map[string]provider.MonitorStats)

	for _, p := range providers {
		if httpProv, ok := p.(*provider.HTTPProvider); ok {
			stats[p.GetName()] = httpProv.Monitor.GetStats()
		}
	}

	return stats
}

// PrintMonitorDashboard returns a formatted dashboard string.
func (c *Client) PrintMonitorDashboard() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("\n=== RPC Monitor Dashboard (Chain: %s) ===\n\n", c.chainID))

	providers := c.router.GetAllProviders(c.chainID)
	for _, p := range providers {
		httpProv, ok := p.(*provider.HTTPProvider)
		if !ok {
			continue
		}

		stats := httpProv.Monitor.GetStats()
		statusStr := map[provider.ProviderStatus]string{
			provider.StatusHealthy:   "✅ HEALTHY",
			provider.StatusDegraded:  "⚠️  DEGRADED",
			provider.StatusThrottled: "🔴 THROTTLED",
			provider.StatusBlocked:   "🚫 BLOCKED",
		}[stats.Status]

		sb.WriteString(fmt.Sprintf("Provider: %s\n", p.GetName()))
		sb.WriteString(fmt.Sprintf("  Status: %s\n", statusStr))
		sb.WriteString(fmt.Sprintf("  Avg Latency: %v\n", stats.AverageLatency))
		sb.WriteString(fmt.Sprintf("  429 Errors: %d\n", stats.ThrottleCount429))
		sb.WriteString(fmt.Sprintf("  403 Errors: %d\n", stats.ThrottleCount403))
		sb.WriteString(fmt.Sprintf("  Usage: %d/%d (%.1f%%)\n",
			stats.RequestsLast24Hours,
			stats.EstimatedDailyLimit,
			stats.UsagePercentage))
		sb.WriteString("\n")
	}

	usage := c.GetUsage()
	sb.WriteString("Budget Status:\n")
	sb.WriteString(fmt.Sprintf("  Quota: %d/%d (%.1f%%)\n",
		usage.TotalCalls, usage.DailyLimit, usage.UsagePercentage))
	sb.WriteString(fmt.Sprintf("  Resets at: %s\n", usage.NextResetAt.Format("15:04:05")))

	return sb.String()
}

func (c *Client) getQuotaAwareProvider() (provider.Provider, error) {
	providers := c.router.GetAllProviders(c.chainID)

	for _, p := range providers {
		if !c.budget.CanUseProvider(c.chainID, p.GetName()) {
			continue
		}

		if httpProv, ok := p.(*provider.HTTPProvider); ok {
			if !httpProv.HasQuotaRemaining() {
				continue
			}

			if c.predictiveRotation && c.coordinator != nil {
				predStats := c.coordinator.GetPredictionStats(c.chainID, p.GetName())
				if predStats.TimeToExhaustion > 0 &&
					predStats.TimeToExhaustion < c.predictionThreshold {
					continue
				}
			}

			return p, nil
		}
	}

	return c.router.GetProvider(c.chainID)
}

func (c *Client) checkAndRotate() {
	if time.Since(c.lastRotationTime) < c.minRotationInterval {
		return
	}

	providers := c.router.GetAllProviders(c.chainID)

	for _, p := range providers {
		if httpProv, ok := p.(*provider.HTTPProvider); ok {
			stats := httpProv.Monitor.GetStats()

			if stats.UsagePercentage > c.rotateThreshold {
				c.router.RotateProvider(c.chainID)
				c.lastRotationTime = time.Now()

				fmt.Printf("🔄 Auto-rotated away from %s (%.1f%% usage)\n",
					p.GetName(), stats.UsagePercentage)
				break
			}
		}
	}
}

func (c *Client) shouldRetryWithRotation(err error) bool {
	if err == nil {
		return false
	}

	errMsg := strings.ToLower(err.Error())

	rotatePatterns := []string{
		"rate limited",
		"throttled",
		"429",
		"quota exceeded",
		"too many requests",
	}

	for _, pattern := range rotatePatterns {
		if strings.Contains(errMsg, pattern) {
			return true
		}
	}

	return false
}
