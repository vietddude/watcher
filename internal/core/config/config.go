package config

import (
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
	redisclient "github.com/vietddude/watcher/internal/infra/redis"
	"github.com/vietddude/watcher/internal/infra/storage/postgres"
)

// AppConfig represents the top-level configuration.
type AppConfig struct {
	Server   ServerConfig       `yaml:"server"`
	Chains   []ChainConfig      `yaml:"chains"`
	Redis    redisclient.Config `yaml:"redis"`
	Logging  LoggingConfig      `yaml:"logging"`
	Database postgres.Config    `yaml:"database"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port int `yaml:"port"`
}

// LoggingConfig holds logging configuration.
type LoggingConfig struct {
	Level  string `yaml:"level"`  // debug, info, warn, error
	Format string `yaml:"format"` // json, text
}

// ChainConfig holds settings for a specific blockchain.
type ChainConfig struct {
	ChainID         domain.ChainID   `yaml:"id"               mapstructure:"id"`
	Type            domain.ChainType `yaml:"type"             mapstructure:"type"` // e.g., "evm", "bitcoin"
	FinalityBlocks  uint64           `yaml:"finality_blocks"  mapstructure:"finality_blocks"`
	ScanInterval    time.Duration    `yaml:"scan_interval"    mapstructure:"scan_interval"`
	RetentionPeriod time.Duration    `yaml:"retention_period" mapstructure:"retention_period"` // 0 = infinite
	RescanRanges    bool             `yaml:"rescan_ranges"    mapstructure:"rescan_ranges"`    // Enable rescan worker
	Providers       []ProviderConfig `yaml:"providers"        mapstructure:"providers"`
}

// ProviderConfig holds settings for an RPC provider.
type ProviderConfig struct {
	Name             string `yaml:"name"              mapstructure:"name"`
	URL              string `yaml:"url"               mapstructure:"url"`
	DailyQuota       int    `yaml:"daily_quota"       mapstructure:"daily_quota"` // 0 = unlimited
	IntervalLimit    int    `yaml:"interval_limit"    mapstructure:"interval_limit"`
	IntervalDuration string `yaml:"interval_duration" mapstructure:"interval_duration"`
}
