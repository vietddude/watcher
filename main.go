package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/vietddude/watcher/internal/infra/rpc"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found")
	}

	INFURA_URL := os.Getenv("INFURA_URL")
	ALCHEMY_URL := os.Getenv("ALCHEMY_URL")
	if INFURA_URL == "" {
		log.Fatalf("INFURA_URL is not set")
	}
	if ALCHEMY_URL == "" {
		log.Fatalf("ALCHEMY_URL is not set")
	}

	ctx := context.Background()

	// 1. Create providers
	provider1 := rpc.NewHTTPProvider("alchemy", ALCHEMY_URL, 30*time.Second)
	provider2 := rpc.NewHTTPProvider("infura", INFURA_URL, 30*time.Second)

	// 2. Setup budget tracker
	budgetAllocation := map[string]float64{
		"ethereum": 1.0,
	}
	budget := rpc.NewBudgetTracker(100000, budgetAllocation)

	// 3. Setup router with proactive rotation strategy
	router := rpc.NewRouterWithStrategy(budget, rpc.RotationProactive)
	router.AddProvider("ethereum", provider1)
	router.AddProvider("ethereum", provider2)

	// 4. Create coordinator for unified budget-router coordination
	coordinator := rpc.NewCoordinator(router, budget)

	// Set up rotation callback to see when rotations happen
	coordinator.SetRotationCallback(func(chainID, from, to, reason string) {
		fmt.Printf("🔄 Rotated from %s to %s: %s\n", from, to, reason)
	})

	// 5. Create client with coordinator (full features)
	client := rpc.NewClientWithCoordinator("ethereum", coordinator)

	fmt.Println("=== Testing RPC with New Features ===\n")

	// 6. Make multiple calls to test rotation and prediction
	for i := 0; i < 5; i++ {
		result, err := client.Call(ctx, "eth_blockNumber", []any{})
		if err != nil {
			log.Printf("Call %d failed: %v", i+1, err)
			continue
		}
		fmt.Printf("Call %d: Block = %s\n", i+1, result.(string))

		time.Sleep(100 * time.Millisecond)
	}

	fmt.Println()

	// 7. Show prediction stats for each provider
	fmt.Println("=== Prediction Stats ===")
	for _, name := range []string{"alchemy", "infura"} {
		stats := coordinator.GetPredictionStats("ethereum", name)
		fmt.Printf("%s:\n", name)
		fmt.Printf("  Rate: %.2f req/min\n", stats.RequestRatePerMin)
		fmt.Printf("  Remaining Quota: %d\n", stats.RemainingQuota)
		if stats.TimeToExhaustion > 0 {
			fmt.Printf("  Predicted Exhaustion: %v\n", stats.TimeToExhaustion.Round(time.Minute))
		} else {
			fmt.Printf("  Predicted Exhaustion: N/A (low activity)\n")
		}
		fmt.Println()
	}

	// 8. Print monitor dashboard
	fmt.Println(client.PrintMonitorDashboard())

	// 9. Show budget usage
	usage := client.GetUsage()
	fmt.Printf("Total calls made: %d / %d (%.1f%%)\n",
		usage.TotalCalls, usage.DailyLimit, usage.UsagePercentage)
}
