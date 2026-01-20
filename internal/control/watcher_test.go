package control

import (
	"context"
	"testing"
	"time"

	"github.com/vietddude/watcher/internal/core/config"
)

func TestWatcher_Lifecycle(t *testing.T) {
	// Setup Config
	cfg := Config{
		Port: 0, // Random port
		Chains: []config.ChainConfig{
			{
				ChainID:        "test-chain-1",
				FinalityBlocks: 5,
				ScanInterval:   100 * time.Millisecond,
				Providers: []config.ProviderConfig{
					{Name: "test", URL: "http://localhost:8545"},
				},
			},
		},
	}

	// Create Watcher
	w, err := NewWatcher(cfg)
	if err != nil {
		t.Fatalf("NewWatcher failed: %v", err)
	}

	if w == nil {
		t.Fatal("Watcher is nil")
	}

	if len(w.indexers) != 1 {
		t.Errorf("expected 1 indexer, got %d", len(w.indexers))
	}

	// Start Watcher
	// We use a context with timeout to stop it
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Wait a bit to let goroutines spin up
	time.Sleep(100 * time.Millisecond)

	// Stop Watcher
	if err := w.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestWatcher_MultiChain(t *testing.T) {
	cfg := Config{
		Port: 0,
		Chains: []config.ChainConfig{
			{
				ChainID:        "chain-1",
				FinalityBlocks: 1,
				Providers:      []config.ProviderConfig{{Name: "p1", URL: "http://loc1"}},
			},
			{
				ChainID:        "chain-2",
				FinalityBlocks: 1,
				Providers:      []config.ProviderConfig{{Name: "p2", URL: "http://loc2"}},
			},
		},
	}

	w, err := NewWatcher(cfg)
	if err != nil {
		t.Fatalf("NewWatcher failed: %v", err)
	}

	if len(w.indexers) != 2 {
		t.Errorf("expected 2 indexers, got %d", len(w.indexers))
	}
}
