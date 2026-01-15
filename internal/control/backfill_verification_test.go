package control

import (
	"context"
	"testing"
	"time"

	"github.com/vietddude/watcher/internal/indexing/backfill"
)

func TestWatcher_BackfillWiring(t *testing.T) {
	// Setup Config with Backfill enabled
	cfg := Config{
		Port: 0,
		Chains: []ChainConfig{
			{
				ChainID:        "test-backfill-chain",
				FinalityBlocks: 1,
				ScanInterval:   100 * time.Millisecond,
				Providers:      []ProviderConfig{{Name: "test", URL: "http://localhost:8545"}},
			},
		},
		Backfill: backfill.ProcessorConfig{
			BlocksPerMinute:   60,
			MinInterval:       1 * time.Millisecond,
			QuotaCheckEnabled: false, // Disable quota check for test
		},
	}

	w, err := NewWatcher(cfg)
	if err != nil {
		t.Fatalf("NewWatcher failed: %v", err)
	}

	// Verify Backfiller is initialized
	if len(w.backfillers) != 1 {
		t.Fatalf("Expected 1 backfiller, got %d", len(w.backfillers))
	}

	bf, ok := w.backfillers["test-backfill-chain"]
	if !ok {
		t.Fatal("Backfiller for chain not found")
	}

	if bf == nil {
		t.Fatal("Backfiller instance is nil")
	}

	// We can't easily verify it's "running" without deeper hooks or checking logs/side effects
	// But ensuring it's in the map guarantees it will be started by Start() loop we verified in code.

	// Let's at least dry-run Start/Stop to ensure no panic
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond) // Let it run briefly

	if err := w.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}
