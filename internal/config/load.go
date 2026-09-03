package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// EnvLogLevel overrides log.level when set.
const EnvLogLevel = "THAWR_LOG_LEVEL"

// Load reads the YAML file at path on top of Default, applies environment
// overrides, and validates. Unknown keys are errors. The returned error
// is a *ValidationError when validation failed, so callers can print
// every problem.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	return Parse(data, os.Getenv)
}

// Parse is Load without the file system: it decodes data on top of
// Default, applies overrides from getenv, and validates.
func Parse(data []byte, getenv func(string) string) (*Config, error) {
	cfg := Default()
	// A user who sets data_dir but not admin_socket expects the socket
	// to follow the data dir, so the default is resolved after decoding.
	cfg.AdminSocket = ""
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("config: parse: %w", err)
	}
	if getenv != nil {
		if v := getenv(EnvLogLevel); v != "" {
			cfg.Log.Level = v
		}
	}
	if cfg.AdminSocket == "" {
		cfg.AdminSocket = filepath.Join(cfg.DataDir, "admin.sock")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}
