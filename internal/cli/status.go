package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/vietddude/watcher/internal/core/config"
	"github.com/vietddude/watcher/internal/infra/storage/postgres"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the current status of all indexed chains",
	Run:   runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) {
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

	rows, err := db.QueryContext(ctx, "SELECT chain_id, block_number, updated_at FROM cursors")
	if err != nil {
		slog.Error("Failed to query cursors", "error", err)
		os.Exit(1)
	}
	defer func() {
		_ = rows.Close()
	}()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', tabwriter.Debug)
	_, _ = fmt.Fprintln(w, "CHAIN\tBLOCK\tUPDATED")

	for rows.Next() {
		var chainID string
		var block int64
		var updatedAt int64
		if err := rows.Scan(&chainID, &block, &updatedAt); err != nil {
			continue
		}
		_, _ = fmt.Fprintf(w, "%s\t%d\t%d\n", chainID, block, updatedAt)
	}
	_ = w.Flush()
}
