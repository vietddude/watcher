package rpc

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/joho/godotenv"
)

func TestRPC(t *testing.T) {
	err := godotenv.Load("../../.env")
	if err != nil {
		t.Fatal("No .env file found")
	}

	INFURA_URL := os.Getenv("INFURA_URL")
	ALCHEMY_URL := os.Getenv("ALCHEMY_URL")
	if INFURA_URL == "" {
		t.Fatal("INFURA_URL is not set")
	}
	if ALCHEMY_URL == "" {
		t.Fatal("ALCHEMY_URL is not set")
	}

	ctx := context.Background()

	// 1. Create providers
	provider1 := NewHTTPProvider("alchemy", ALCHEMY_URL, 30*time.Second)
	provider2 := NewHTTPProvider("infura", INFURA_URL, 30*time.Second)

	// 2. Setup budget tracker
	budgetAllocation := map[string]float64{
		"ethereum": 1.0,
	}
	budget := NewBudgetTracker(100000, budgetAllocation)

	// 3. Setup router with proactive rotation strategy
	router := NewRouterWithStrategy(budget, RotationProactive)
	router.AddProvider("ethereum", provider1)
	router.AddProvider("ethereum", provider2)

	// 4. Create coordinator for unified budget-router coordination
	coordinator := NewCoordinator(router, budget)

	// Set up rotation callback to see when rotations happen
	coordinator.SetRotationCallback(func(chainID, from, to, reason string) {
		t.Logf("🔄 Rotated from %s to %s: %s\n", from, to, reason)
	})

	// 5. Create client with coordinator (full features)
	client := NewClientWithCoordinator("ethereum", coordinator)

	// 6. Make multiple calls to test rotation and prediction
	for i := 0; i < 5; i++ {
		result, err := client.Call(ctx, "eth_blockNumber", []any{})
		if err != nil {
			t.Errorf("Call %d failed: %v", i+1, err)
			continue
		}
		t.Logf("Call %d: Block = %s\n", i+1, result.(string))

		time.Sleep(100 * time.Millisecond)
	}

	// 7. Show prediction stats for each provider
	for _, name := range []string{"alchemy", "infura"} {
		stats := coordinator.GetPredictionStats("ethereum", name)
		t.Logf("%s:\n", name)
		t.Logf("  Rate: %.2f req/min\n", stats.RequestRatePerMin)
		t.Logf("  Remaining Quota: %d\n", stats.RemainingQuota)
		if stats.TimeToExhaustion > 0 {
			t.Logf("  Predicted Exhaustion: %v\n", stats.TimeToExhaustion.Round(time.Minute))
		} else {
			t.Logf("  Predicted Exhaustion: N/A (low activity)\n")
		}
	}

	// 8. Print monitor dashboard
	t.Logf("%s", client.PrintMonitorDashboard())

	// 9. Show budget usage
	usage := client.GetUsage()
	t.Logf("Total calls made: %d / %d (%.1f%%)\n",
		usage.TotalCalls, usage.DailyLimit, usage.UsagePercentage)
}
