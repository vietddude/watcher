package config

import (
	"os"
	"testing"
)

func TestLoad_EnvSubstitution(t *testing.T) {
	// Setup env var
	os.Setenv("TEST_DB_URL", "postgres://user:pass@localhost:5433/db")
	defer os.Unsetenv("TEST_DB_URL")

	// Create temp config file
	configContent := `
database:
  url: ${TEST_DB_URL}
`
	tmpFile, err := os.CreateTemp("", "config_*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(configContent)); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	// Load config
	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Database.URL != "postgres://user:pass@localhost:5433/db" {
		t.Errorf("Expected URL postgres://user:pass@localhost:5433/db, got %s", cfg.Database.URL)
	}
}
