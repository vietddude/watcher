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
	redisclient "github.com/vietddude/watcher/internal/infra/redis"
)

func main() {
	// Parse flags
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	rescanRanges := flag.Bool("rescan-ranges", true, "Enable rescan range processing from Redis")
	logLevel := flag.String("log-level", "", "Log level (debug, info, warn, error) - overrides config")
	flag.Parse()

	// Load Configuration first (before setting up logger)
	cfg, err := config.Load(*configPath)
	if err != nil {
		// Fall back to default logger for config load errors
		stylelog.InitDefault()
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	// Determine log level (CLI flag > config > default)
	level := cfg.Logging.Level
	if *logLevel != "" {
		level = *logLevel
	}
	if level == "" {
		level = "info"
	}

	// Parse log level
	var slogLevel slog.Level
	switch level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	// Initialize stylelog with tint.Options for level control
	stylelog.InitDefault(&tint.Options{Level: slogLevel})
	slog.Info("Logger initialized", "level", level)

	// Transform config
	controlCfg := control.Config{
		Port:                cfg.Server.Port,
		Chains:              make([]config.ChainConfig, len(cfg.Chains)),
		RescanRangesEnabled: *rescanRanges,
		Redis: redisclient.Config{
			URL:      cfg.Redis.URL,
			Password: cfg.Redis.Password,
		},
	}

	for i, c := range cfg.Chains {
		providers := make([]config.ProviderConfig, len(c.Providers))
		for j, p := range c.Providers {
			providers[j] = config.ProviderConfig{
				Name: p.Name,
				URL:  p.URL,
			}
		}
		controlCfg.Chains[i] = config.ChainConfig{
			ChainID:        c.ChainID,
			Type:           c.Type,
			InternalCode:   c.InternalCode,
			FinalityBlocks: c.FinalityBlocks,
			ScanInterval:   c.ScanInterval,
			RescanRanges:   c.RescanRanges,
			Providers:      providers,
		}
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
