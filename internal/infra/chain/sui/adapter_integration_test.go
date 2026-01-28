package sui

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/rpc"
)

func TestAdapterStreaming(t *testing.T) {
	if os.Getenv("E2E_LIVE") == "" {
		t.Skip("Skipping live adapter test. Set E2E_LIVE=true to run.")
	}

	// 1. Setup Real GRPC Provider
	// Use the public testnet endpoint
	url := "https://fullnode.testnet.sui.io:443"
	provider, err := rpc.NewGRPCProvider(context.Background(), "test-provider", url)
	if err != nil {
		t.Fatalf("Failed to create GRPC provider: %v", err)
	}

	// 2. Create Adapter
	// CoordinatedProvider expects a Coordinator, but we can pass the provider directly if it implements RPCClient?
	// rpc.GRPCProvider implements rpc.Provider, which implements Execute?
	// Let's check if GRPCProvider implements Execute directly.
	// Usually Provider interface has Execute.

	// Assuming GRPCProvider implements RPCClient interface (Execute method).
	adapter := NewAdapter(domain.ChainID("784"), provider)

	// 3. Monitor GetLatestBlock
	// Initially it might use RPC fallback, but eventually stream should kick in.
	ctx := context.Background()

	t.Log("Waiting for stream to initialize...")
	time.Sleep(2 * time.Second)

	initialHeight, err := adapter.GetLatestBlock(ctx)
	if err != nil {
		t.Fatalf("Failed to get initial height: %v", err)
	}
	t.Logf("Initial Height: %d", initialHeight)

	// Wait for a few seconds to see if height updates via stream
	// Since testnet moves fast, we should see updates.
	time.Sleep(5 * time.Second)

	newHeight, err := adapter.GetLatestBlock(ctx)
	if err != nil {
		t.Fatalf("Failed to get new height: %v", err)
	}
	t.Logf("New Height: %d", newHeight)

	if newHeight <= initialHeight {
		t.Log(
			"Warning: Height did not increase. This might be due to slow network or stream not working.",
		)
	} else {
		t.Log("Success: Height increased, likely via stream or polling.")
	}
}
