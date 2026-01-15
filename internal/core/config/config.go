package config

import "time"

// AppConfig represents the top-level configuration.
type AppConfig struct {
	Server ServerConfig  `yaml:"server"`
	Chains []ChainConfig `yaml:"chains"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port int `yaml:"port"`
}

// ChainConfig holds settings for a specific blockchain.
type ChainConfig struct {
	ID             string        `yaml:"id"`
	Type           string        `yaml:"type"` // e.g., "evm", "bitcoin"
	RPCURL         string        `yaml:"rpc_url"`
	FinalityBlocks uint64        `yaml:"finality_blocks"`
	ScanInterval   time.Duration `yaml:"scan_interval"`
}
