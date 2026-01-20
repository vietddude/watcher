package rpc

import (
	"context"
	"fmt"
	"strings"

	"github.com/vietddude/watcher/internal/infra/rpc/budget"
	"github.com/vietddude/watcher/internal/infra/rpc/provider"
	"github.com/vietddude/watcher/internal/infra/rpc/routing"
)

// Client is the high-level interface for making RPC calls.
// This is what application layers should use.
type Client struct {
	chainID     string
	coordinator *budget.Coordinator
}

// NewClient creates a new RPC client.
// It automatically initializes a default Coordinator.
func NewClient(chainID string, router routing.Router, b budget.BudgetTracker) *Client {
	coordinator := budget.NewCoordinator(router, b)
	return &Client{
		chainID:     chainID,
		coordinator: coordinator,
	}
}

// NewClientWithCoordinator creates a client with full coordinator support.
func NewClientWithCoordinator(chainID string, coordinator *budget.Coordinator) *Client {
	return &Client{
		chainID:     chainID,
		coordinator: coordinator,
	}
}

// Call makes an RPC call with automatic failover and retry.
func (c *Client) Call(ctx context.Context, method string, params []any) (any, error) {
	return c.coordinator.Call(ctx, c.chainID, method, params)
}

// CallWithFailover executes an RPC call with failover logic.
// This is now an alias for Call, as Call includes robust failover.
func (c *Client) CallWithFailover(ctx context.Context, method string, params []any) (any, error) {
	return c.Call(ctx, method, params)
}

// ForceRotation forces a provider rotation.
func (c *Client) ForceRotation() error {
	_, err := c.coordinator.ForceRotate(c.chainID, "")
	return err
}

// SetRotationStrategy updates the rotation strategy.
// Note: This only works if the underlying system supports it.
func (c *Client) SetRotationStrategy(strategy routing.RotationStrategy) {
	// Pass through to coordinator's router if possible
	if router, ok := c.coordinator.GetRouter().(*routing.DefaultRouter); ok {
		router.SetRotationStrategy(strategy)
	}
}

// GetUsage returns current budget usage.
func (c *Client) GetUsage() budget.UsageStats {
	return c.coordinator.GetBudget().GetUsage(c.chainID)
}

// GetProviderStats returns monitoring stats for all providers.
func (c *Client) GetProviderStats() map[string]provider.MonitorStats {
	providers := c.coordinator.GetRouter().GetAllProviders(c.chainID)
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

	fmt.Fprintf(&sb, "\n=== RPC Monitor Dashboard (Chain: %s) ===\n\n", c.chainID)

	providers := c.coordinator.GetRouter().GetAllProviders(c.chainID)
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

		fmt.Fprintf(&sb, "Provider: %s\n", p.GetName())
		fmt.Fprintf(&sb, "  Status: %s\n", statusStr)
		fmt.Fprintf(&sb, "  Avg Latency: %v\n", stats.AverageLatency)
		fmt.Fprintf(&sb, "  429 Errors: %d\n", stats.ThrottleCount429)
		fmt.Fprintf(&sb, "  403 Errors: %d\n", stats.ThrottleCount403)
		fmt.Fprintf(&sb, "  Usage: %d/%d (%.1f%%)\n",
			stats.RequestsLast24Hours,
			stats.EstimatedDailyLimit,
			stats.UsagePercentage)
		fmt.Fprintf(&sb, "\n")
	}

	usage := c.GetUsage()
	fmt.Fprintf(&sb, "Budget Status:\n")
	fmt.Fprintf(&sb, "  Quota: %d/%d (%.1f%%)\n",
		usage.TotalCalls, usage.DailyLimit, usage.UsagePercentage)
	fmt.Fprintf(&sb, "  Resets at: %s\n", usage.NextResetAt.Format("15:04:05"))
	return sb.String()
}
