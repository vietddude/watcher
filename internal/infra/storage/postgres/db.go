package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"

	"github.com/vietddude/watcher/internal/indexing/metrics"
	"github.com/vietddude/watcher/internal/infra/storage/postgres/sqlc"
)

// Config holds PostgreSQL connection configuration.
type Config struct {
	URL      string `yaml:"url"`
	MaxConns int    `yaml:"max_conns"`
	MinConns int    `yaml:"min_conns"`
}

// DB wraps the PostgreSQL connection.
type DB struct {
	*sql.DB
	Queries *sqlc.Queries
}

// NewDB creates a new database connection.
func NewDB(ctx context.Context, cfg Config) (*DB, error) {
	db, err := sql.Open("postgres", cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set pool configuration
	if cfg.MaxConns > 0 {
		db.SetMaxOpenConns(cfg.MaxConns)
	} else {
		db.SetMaxOpenConns(10)
	}

	if cfg.MinConns > 0 {
		db.SetMaxIdleConns(cfg.MinConns)
	} else {
		db.SetMaxIdleConns(2)
	}

	db.SetConnMaxLifetime(time.Hour)
	db.SetConnMaxIdleTime(30 * time.Minute)

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{
		DB:      db,
		Queries: sqlc.New(db),
	}, nil
}

// StartMetricsCollector starts a background goroutine to collect DB metrics.
func (db *DB) StartMetricsCollector(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stats := db.Stats()
				// Calculate usage percentage: (OpenConnections / MaxOpenConnections) * 100
				// Note: MaxOpenConnections might be 0 (unlimited), handle that.
				if stats.MaxOpenConnections > 0 {
					usage := float64(
						stats.OpenConnections,
					) / float64(
						stats.MaxOpenConnections,
					) * 100
					metrics.DBConnectionPoolUsage.Set(usage)
				}
			}
		}
	}()
}

// Health checks if the database is healthy.
func (db *DB) Health(ctx context.Context) error {
	return db.PingContext(ctx)
}
