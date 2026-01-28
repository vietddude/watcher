package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/vietddude/watcher/internal/control"
	"github.com/vietddude/watcher/internal/core/config"
	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/storage/postgres"
)

func TestGracefulShutdown(t *testing.T) {
	// Simple config with no real work to do but enough to start components
	cfg := control.Config{
		Database: postgres.Config{
			URL: "postgres://watcher:watcher123@localhost:5432/watcher?sslmode=disable",
		},
		Chains: []config.ChainConfig{
			{
				ChainID:      domain.EthereumMainnet,
				Type:         domain.ChainTypeEVM,
				ScanInterval: 1 * time.Second,
				Providers: []config.ProviderConfig{
					{Name: "stub", URL: "http://localhost:8545"},
				},
			},
		},
	}

	watcher, err := control.NewWatcher(cfg)
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	startError := make(chan error, 1)
	go func() {
		startError <- watcher.Start(ctx)
	}()

	// Let it run for a bit
	time.Sleep(2 * time.Second)

	// Trigger shutdown
	cancel()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	err = watcher.Stop(stopCtx)
	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}

	select {
	case err := <-startError:
		if err != nil && err != context.Canceled {
			t.Errorf("Watcher.Start returned error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("Watcher.Start did not return within 10s of Stop")
	}
}
