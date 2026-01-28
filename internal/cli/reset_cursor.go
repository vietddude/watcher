package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/vietddude/watcher/internal/core/config"
	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/storage/postgres"
)

var resetCursorCmd = &cobra.Command{
	Use:   "reset-cursor [chain_id] [block_height]",
	Short: "Reset the cursor for a specific chain to a given block height",
	Args:  cobra.ExactArgs(2),
	Run:   runResetCursor,
}

func init() {
	rootCmd.AddCommand(resetCursorCmd)
}

func runResetCursor(cmd *cobra.Command, args []string) {
	chainID := domain.ChainID(args[0])
	height, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		fmt.Printf("Invalid block height: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()
	db, err := postgres.NewDB(ctx, cfg.Database)
	if err != nil {
		slog.Error("Failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer func() {
		_ = db.Close()
	}()

	// Update cursor
	// We'll use a direct SQL query or a repo if available.
	// Since this is a CLI tool, direct SQL is often cleaner for simple overrides.
	query := "INSERT INTO cursors (chain_id, block_number, updated_at) VALUES ($1, $2, extract(epoch from now())::bigint) ON CONFLICT (chain_id) DO UPDATE SET block_number = EXCLUDED.block_number, updated_at = EXCLUDED.updated_at"
	_, err = db.ExecContext(ctx, query, string(chainID), height)
	if err != nil {
		slog.Error("Failed to reset cursor", "error", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully reset cursor for %s to block %d\n", chainID, height)
}
