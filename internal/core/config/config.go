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
	ChainID            domain.ChainID            `yaml:"id"                  mapstructure:"id"`
	Type               domain.ChainType          `yaml:"type"                mapstructure:"type"` // e.g., "evm", "bitcoin"
	FinalityBlocks     uint64                    `yaml:"finality_blocks"     mapstructure:"finality_blocks"`
	ScanInterval       time.Duration             `yaml:"scan_interval"       mapstructure:"scan_interval"`
	RetentionPeriod    time.Duration             `yaml:"retention_period"    mapstructure:"retention_period"` // 0 = infinite
	RescanRanges       bool                      `yaml:"rescan_ranges"       mapstructure:"rescan_ranges"`    // Enable rescan worker
	Providers          []ProviderConfig          `yaml:"providers"           mapstructure:"providers"`
	AdaptiveThrottling *AdaptiveThrottlingConfig `yaml:"adaptive_throttling" mapstructure:"adaptive_throttling"`
}

// ProviderConfig holds settings for an RPC provider.
type ProviderConfig struct {
	Name             string `yaml:"name"              mapstructure:"name"`
	URL              string `yaml:"url"               mapstructure:"url"`
	DailyQuota       int    `yaml:"daily_quota"       mapstructure:"daily_quota"` // 0 = unlimited
	IntervalLimit    int    `yaml:"interval_limit"    mapstructure:"interval_limit"`
	IntervalDuration string `yaml:"interval_duration" mapstructure:"interval_duration"`
}

// AdaptiveThrottlingConfig holds settings for adaptive throttling.
type AdaptiveThrottlingConfig struct {
	Enabled           bool          `yaml:"enabled"             mapstructure:"enabled"`
	MinScanInterval   time.Duration `yaml:"min_scan_interval"   mapstructure:"min_scan_interval"`
	MaxScanInterval   time.Duration `yaml:"max_scan_interval"   mapstructure:"max_scan_interval"`
	HeadCacheTTL      time.Duration `yaml:"head_cache_ttl"      mapstructure:"head_cache_ttl"`
	BatchEnabled      bool          `yaml:"batch_enabled"       mapstructure:"batch_enabled"`
	MaxBatchSize      int           `yaml:"max_batch_size"      mapstructure:"max_batch_size"`
	LagBurstThreshold int64         `yaml:"lag_burst_threshold" mapstructure:"lag_burst_threshold"`
}
