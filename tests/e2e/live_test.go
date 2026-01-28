package e2e

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"
	"github.com/vietddude/watcher/internal/control"
	"github.com/vietddude/watcher/internal/core/config"
	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/storage/postgres"
)

const (
	TestDBURL = "postgres://watcher:watcher123@localhost:5432/watcher_test?sslmode=disable"
	// Binance Hot Wallet (EVM) - lowercased for DB matching
	BinanceWallet = "0x28c6c06298d514db089934071355e5743bf21d60"
)

func setupTestDB(t *testing.T, dbName string) *sql.DB {
	// Root connection to create test DB
	rootDB, err := sql.Open("postgres", "postgres://watcher:watcher123@localhost:5432/postgres?sslmode=disable")
	if err != nil {
		t.Fatalf("Failed to connect to root postgres: %v", err)
	}
	defer rootDB.Close()

	// Drop and recreate test DB
	_, _ = rootDB.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	_, err = rootDB.Exec(fmt.Sprintf("CREATE DATABASE %s", dbName))
	if err != nil {
		t.Fatalf("Failed to create test database %s: %v", dbName, err)
	}

	// Connect to test DB
	testURL := fmt.Sprintf("postgres://watcher:watcher123@localhost:5432/%s?sslmode=disable", dbName)
	db, err := sql.Open("postgres", testURL)
	if err != nil {
		t.Fatalf("Failed to connect to test database %s: %v", dbName, err)
	}

	// Run migrations
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("Failed to set goose dialect: %v", err)
	}
	// Path to migrations from tests/e2e directory
	if err := goose.Up(db, "../../migrations"); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	return db
}

func TestEVMIndexing_Live(t *testing.T) {
	if os.Getenv("E2E_LIVE") == "" {
		t.Skip("Skipping live E2E test. Set E2E_LIVE=true to run.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	dbName := "watcher_test_evm"
	testDB := setupTestDB(t, dbName)
	defer testDB.Close()

	// Seed monitored address
	_, err := testDB.Exec("INSERT INTO wallet_addresses (address, network_type, standard) VALUES ($1, $2, $3)", BinanceWallet, "evm", "erc20")
	if err != nil {
		t.Fatalf("Failed to seed wallet address: %v", err)
	}

	// Basic config for live RPC
	cfg := control.Config{
		Port: 0,
		Database: postgres.Config{
			URL: fmt.Sprintf("postgres://watcher:watcher123@localhost:5432/%s?sslmode=disable", dbName),
		},
		Chains: []config.ChainConfig{
			{
				ChainID:        domain.EthereumMainnet,
				Type:           domain.ChainTypeEVM,
				FinalityBlocks: 1,
				ScanInterval:   10 * time.Second,
				Providers: []config.ProviderConfig{
					{
						Name: "public",
						URL:  "https://ethereum-rpc.publicnode.com",
					},
				},
			},
		},
	}

	watcher, err := control.NewWatcher(cfg)
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	// Start watcher in background
	go func() {
		if err := watcher.Start(ctx); err != nil {
			fmt.Printf("Watcher error: %v\n", err)
		}
	}()

	// Wait for indexer to find at least one transaction
	// Binance wallet is extremely active, so this should happen quickly if it's at the head
	found := false
	for i := 0; i < 30; i++ { // Check for ~5 minutes
		time.Sleep(10 * time.Second)
		var count int
		err := testDB.QueryRow("SELECT COUNT(*) FROM transactions WHERE to_address = $1 OR from_address = $1", BinanceWallet).Scan(&count)
		if err == nil && count > 0 {
			t.Logf("SUCCESS: Found %d transactions for Binance wallet in DB", count)
			found = true
			break
		} else if err != nil {
			t.Logf("Query error: %v", err)
		} else {
			// Check if ANY transactions exist to verify persistence is working
			var anyCount int
			_ = testDB.QueryRow("SELECT COUNT(*) FROM transactions").Scan(&anyCount)
			t.Logf("Waiting... iteration %d, DB currently has %d transactions total (wanted: %s)", i, anyCount, BinanceWallet)
		}
	}

	if !found {
		t.Error("Timed out waiting for transactions to be indexed from live network")
	}

	cancel()
	_ = watcher.Stop(context.Background())
}

func TestSuiIndexing_Live(t *testing.T) {
	if os.Getenv("E2E_LIVE") == "" {
		t.Skip("Skipping live E2E test. Set E2E_LIVE=true to run.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	dbName := "watcher_test_sui"
	testDB := setupTestDB(t, dbName)
	defer testDB.Close()

	// Generic active Sui address (or we can just watch "any" and see if blocks come in)
	// For E2E we usually want to verify we can capture *something*.
	// Let's use a known system address or just watch all and verify block count.

	cfg := control.Config{
		Port: 0,
		Database: postgres.Config{
			URL: fmt.Sprintf("postgres://watcher:watcher123@localhost:5432/%s?sslmode=disable", dbName),
		},
		Chains: []config.ChainConfig{
			{
				ChainID:        domain.SuiMainnet,
				Type:           domain.ChainTypeSui,
				FinalityBlocks: 0,
				ScanInterval:   1 * time.Second,
				Providers: []config.ProviderConfig{
					{
						Name: "fullnode",
						URL:  "https://fullnode.mainnet.sui.io:443",
					},
				},
				GapJumpThreshold: 5000,
			},
		},
	}

	watcher, err := control.NewWatcher(cfg)
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	go func() {
		if err := watcher.Start(ctx); err != nil {
			fmt.Printf("Watcher error: %v\n", err)
		}
	}()

	// Wait for indexer to save some blocks
	found := false
	for i := 0; i < 15; i++ {
		time.Sleep(20 * time.Second)
		var count int
		err := testDB.QueryRow("SELECT COUNT(*) FROM blocks WHERE chain_id = $1", domain.SuiMainnet).Scan(&count)
		if err == nil && count > 0 {
			t.Logf("SUCCESS: Found %d Sui blocks in DB", count)
			found = true
			break
		} else {
			t.Logf("Waiting... iteration %d, no Sui blocks found yet", i)
		}
	}

	if !found {
		t.Error("Timed out waiting for Sui blocks to be indexed")
	}

	cancel()
	_ = watcher.Stop(context.Background())
}
