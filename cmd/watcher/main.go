package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lmittmann/tint"
	"github.com/vietddude/stylelog"
	"github.com/vietddude/watcher/internal/control"
	"github.com/vietddude/watcher/internal/core/config"
)

func main() {
	// Parse flags
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	rescanRanges := flag.Bool("rescan-ranges", true, "Enable rescan range processing from Redis")
	isDebug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	// Load Configuration first (before setting up logger)
	cfg, err := config.Load(*configPath)
	if err != nil {
		// Fall back to default logger for config load errors
		stylelog.InitDefault()
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	// Simplifed logging logic (debug < info)
	slogLevel := slog.LevelInfo
	if *isDebug || cfg.Logging.Level == "debug" {
		slogLevel = slog.LevelDebug
	}

	// Initialize stylelog with tint.Options for level control
	stylelog.InitDefault(
		&tint.Options{
			Level:      slogLevel,
			TimeFormat: time.RFC3339,
		})
	slog.Info("Logger initialized", "level", slogLevel.String())

	// Transform config
	controlCfg := control.Config{
		Port:                cfg.Server.Port,
		Chains:              cfg.Chains,
		RescanRangesEnabled: *rescanRanges,
		Redis:               cfg.Redis,
		Database:            cfg.Database,
	}

	// Initialize Watcher
	app, err := control.NewWatcher(controlCfg)
	if err != nil {
		slog.Error("Failed to initialize Watcher", "error", err)
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
		slog.Error("Failed to start Watcher", "error", err)
		os.Exit(1)
	}

	// Wait for Signal
	sig := <-sigChan
	slog.Info("Received signal, shutting down...", "signal", sig)

	// Graceful Shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := app.Stop(shutdownCtx); err != nil {
		slog.Error("Error during shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("Watcher stopped gracefully")
}
