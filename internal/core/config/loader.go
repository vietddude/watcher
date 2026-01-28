package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v2"
)

// Load reads configuration from a YAML file.
func Load(path string) (*AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg AppConfig
	// Expand environment variables in the YAML content
	expandedData := os.ExpandEnv(string(data))
	if err := yaml.Unmarshal([]byte(expandedData), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set defaults if necessary
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}

	for i := range cfg.Chains {
		if cfg.Chains[i].ScanInterval == 0 {
			cfg.Chains[i].ScanInterval = 10 * time.Second
		}
		if cfg.Chains[i].FinalityBlocks == 0 {
			cfg.Chains[i].FinalityBlocks = 1
		}
	}

	return &cfg, nil
}
