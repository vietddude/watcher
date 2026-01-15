package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

// Config holds PostgreSQL connection configuration.
type Config struct {
	URL      string `yaml:"url"`
	MaxConns int    `yaml:"max_conns"`
	MinConns int    `yaml:"min_conns"`
}

// DB wraps the PostgreSQL connection.
type DB struct {
	*sqlx.DB
}

// NewDB creates a new database connection.
func NewDB(ctx context.Context, cfg Config) (*DB, error) {
	db, err := sqlx.ConnectContext(ctx, "postgres", cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
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
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{DB: db}, nil
}

// Health checks if the database is healthy.
func (db *DB) Health(ctx context.Context) error {
	return db.PingContext(ctx)
}
