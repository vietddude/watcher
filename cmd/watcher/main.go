package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vietddude/stylelog"
	"github.com/vietddude/watcher/internal/control"
	"github.com/vietddude/watcher/internal/core/config"
)

func main() {
	// Parse flags
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	// Setup logging
	logger := stylelog.InitDefault()

	logger.Info("Starting Watcher...", "config", *configPath)

	// Load Configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	// Transform config
	controlCfg := control.Config{
		ServerPort: cfg.Server.Port,
		Chains:     make([]control.ChainConfig, len(cfg.Chains)),
	}

	for i, c := range cfg.Chains {
		controlCfg.Chains[i] = control.ChainConfig{
			ChainID:        c.ID,
			RPCURL:         c.RPCURL,
			FinalityBlocks: c.FinalityBlocks,
			ScanInterval:   c.ScanInterval,
		}
	}

	// Initialize Watcher
	app, err := control.NewWatcher(controlCfg)
	if err != nil {
		logger.Error("Failed to initialize Watcher", "error", err)
		os.Exit(1)
	}

	// Setup Context with Cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle OS Signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start App
	if err := app.Start(ctx); err != nil {
		logger.Error("Failed to start Watcher", "error", err)
		os.Exit(1)
	}

	// Wait for Signal
	sig := <-sigChan
	logger.Info("Received signal, shutting down...", "signal", sig)

	// Graceful Shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := app.Stop(shutdownCtx); err != nil {
		logger.Error("Error during shutdown", "error", err)
		os.Exit(1)
	}

	logger.Info("Watcher stopped gracefully")
}
