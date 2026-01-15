package config

import "time"

// AppConfig represents the top-level configuration.
type AppConfig struct {
	Server   ServerConfig   `yaml:"server"`
	Chains   []ChainConfig  `yaml:"chains"`
	Redis    RedisConfig    `yaml:"redis"`
	Logging  LoggingConfig  `yaml:"logging"`
	Database DatabaseConfig `yaml:"database"`
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

// RedisConfig holds Redis connection settings.
type RedisConfig struct {
	URL      string `yaml:"url"`
	Password string `yaml:"password"`
}

// DatabaseConfig holds PostgreSQL connection settings.
type DatabaseConfig struct {
	URL      string `yaml:"url"`
	MaxConns int    `yaml:"max_conns"`
	MinConns int    `yaml:"min_conns"`
}

// ChainConfig holds settings for a specific blockchain.
type ChainConfig struct {
	ID             string           `yaml:"id"`
	Type           string           `yaml:"type"`          // e.g., "evm", "bitcoin"
	InternalCode   string           `yaml:"internal_code"` // e.g., "ETHEREUM_MAINNET"
	FinalityBlocks uint64           `yaml:"finality_blocks"`
	ScanInterval   time.Duration    `yaml:"scan_interval"`
	RescanRanges   bool             `yaml:"rescan_ranges"` // Enable rescan worker
	Providers      []ProviderConfig `yaml:"providers"`
}

// ProviderConfig holds settings for an RPC provider.
type ProviderConfig struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}
