package config

import "time"

type Config struct {
	Chains   []ChainConfig  `yaml:"chains"`
	Database DatabaseConfig `yaml:"database"`
	Emitter  EmitterConfig  `yaml:"emitter"`
	RPC      RPCConfig      `yaml:"rpc"`
}

type ChainConfig struct {
	ID             string        `yaml:"id"`
	Name           string        `yaml:"name"`
	Type           string        `yaml:"type"` // "evm", "bitcoin", "sui"
	RPCEndpoints   []string      `yaml:"rpc_endpoints"`
	StartBlock     uint64        `yaml:"start_block"`
	FinalityBlocks uint64        `yaml:"finality_blocks"`
	ScanInterval   time.Duration `yaml:"scan_interval"`
	BatchSize      int           `yaml:"batch_size"`
	Priority       int           `yaml:"priority"`
	Addresses      []string      `yaml:"addresses"`
	Enabled        bool          `yaml:"enabled"`
}

type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Database string `yaml:"database"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	SSLMode  string `yaml:"ssl_mode"`
}

type EmitterConfig struct {
	Type   string                 `yaml:"type"` // "kafka", "webhook"
	Config map[string]interface{} `yaml:"config"`
}

type RPCConfig struct {
	Timeout         time.Duration      `yaml:"timeout"`
	MaxRetries      int                `yaml:"max_retries"`
	DailyQuotaLimit int                `yaml:"daily_quota_limit"`
	BudgetAllocatio map[string]float64 `yaml:"budget_allocation"` // chainID -> percentage
}
