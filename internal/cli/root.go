package cli

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/lmittmann/tint"
	"github.com/spf13/cobra"
	"github.com/vietddude/stylelog"
	"github.com/vietddude/watcher/internal/control"
	"github.com/vietddude/watcher/internal/core/config"
)

var (
	cfgPath      string
	isDebug      bool
	rescanRanges bool
)

var rootCmd = &cobra.Command{
	Use:   "watcher",
	Short: "Watcher indexing service",
	Long:  `Watcher is a high-performance blockchain indexing service for EVM, Sui, and Bitcoin.`,
	Run:   runWatcher,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgPath, "config", "config.yaml", "config file (default is config.yaml)")
	rootCmd.PersistentFlags().BoolVar(&isDebug, "debug", false, "enable debug logging")
	rootCmd.PersistentFlags().BoolVar(&rescanRanges, "rescan-ranges", true, "enable rescan range processing")
}

func runWatcher(cmd *cobra.Command, args []string) {
	_ = godotenv.Load()

	// Load Configuration
	cfg, err := config.Load(cfgPath)
	if err != nil {
		stylelog.InitDefault()
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	// Setup logging
	slogLevel := slog.LevelInfo
	if isDebug || cfg.Logging.Level == "debug" {
		slogLevel = slog.LevelDebug
	}

	stylelog.InitDefault(&tint.Options{
		Level:      slogLevel,
		TimeFormat: time.RFC3339,
	})

	// Transform config
	controlCfg := control.Config{
		Port:                cfg.Server.Port,
		Chains:              cfg.Chains,
		RescanRangesEnabled: rescanRanges,
		Redis:               cfg.Redis,
		Database:            cfg.Database,
	}

	// Initialize Watcher
	app, err := control.NewWatcher(controlCfg)
	if err != nil {
		slog.Error("Failed to initialize Watcher", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	if err := app.Start(ctx); err != nil {
		slog.Error("Failed to start Watcher", "error", err)
		os.Exit(1)
	}

	slog.Info("Watcher started", "config", cfgPath)

	sig := <-sigChan
	slog.Info("Received signal, shutting down...", "signal", sig)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	if err := app.Stop(shutdownCtx); err != nil {
		slog.Error("Error during shutdown", "error", err)
		os.Exit(1)
	}
}
