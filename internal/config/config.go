package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds the application configuration.
type Config struct {
	Port             int    `yaml:"port"`
	BoardsDir        string `yaml:"boards_dir"`
	GitAutoCommit    bool   `yaml:"git_auto_commit"`
	GitAutoPush      bool   `yaml:"git_auto_push"`
	HeartbeatTimeout string `yaml:"heartbeat_timeout"`
	CORSOrigin       string `yaml:"cors_origin"`
}

// defaults returns a Config with default values.
func defaults() *Config {
	return &Config{
		Port:             8080,
		BoardsDir:        "", // No default — must be configured
		GitAutoCommit:    true,
		GitAutoPush:      false,
		HeartbeatTimeout: "30m",
		CORSOrigin:       "http://localhost:5173",
	}
}

// Validate checks that required configuration fields are set.
func (c *Config) Validate() error {
	if c.BoardsDir == "" {
		return fmt.Errorf("boards_dir is required: configure it in config.yaml or set CONTEXTMATRIX_BOARDS_DIR")
	}
	if _, err := time.ParseDuration(c.HeartbeatTimeout); err != nil {
		return fmt.Errorf("invalid heartbeat_timeout %q: %w", c.HeartbeatTimeout, err)
	}
	return nil
}

// Load reads configuration from the given YAML file and applies environment overrides.
func Load(path string) (*Config, error) {
	cfg := defaults()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			applyEnvOverrides(cfg)
			boardsDir, err := expandTilde(cfg.BoardsDir)
			if err != nil {
				return nil, err
			}
			cfg.BoardsDir = boardsDir
			if err := cfg.Validate(); err != nil {
				return nil, err
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	applyEnvOverrides(cfg)

	// Expand ~ in paths
	boardsDir, err := expandTilde(cfg.BoardsDir)
	if err != nil {
		return nil, err
	}
	cfg.BoardsDir = boardsDir

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// applyEnvOverrides applies environment variable overrides to the config.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("CONTEXTMATRIX_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Port = port
		} else {
			slog.Warn("ignoring invalid CONTEXTMATRIX_PORT", "value", v, "error", err)
		}
	}
	if v := os.Getenv("CONTEXTMATRIX_BOARDS_DIR"); v != "" {
		cfg.BoardsDir = v
	}
	if v := os.Getenv("CONTEXTMATRIX_GIT_AUTO_COMMIT"); v != "" {
		cfg.GitAutoCommit = v == "true" || v == "1"
	}
	if v := os.Getenv("CONTEXTMATRIX_GIT_AUTO_PUSH"); v != "" {
		cfg.GitAutoPush = v == "true" || v == "1"
	}
	if v := os.Getenv("CONTEXTMATRIX_HEARTBEAT_TIMEOUT"); v != "" {
		cfg.HeartbeatTimeout = v
	}
	if v := os.Getenv("CONTEXTMATRIX_CORS_ORIGIN"); v != "" {
		cfg.CORSOrigin = v
	}
}

// HeartbeatDuration parses HeartbeatTimeout as a time.Duration.
func (c *Config) HeartbeatDuration() (time.Duration, error) {
	return time.ParseDuration(c.HeartbeatTimeout)
}

// expandTilde expands a leading ~ in a path to the user's home directory.
func expandTilde(path string) (string, error) {
	if path == "" {
		return path, nil
	}
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand ~: %w", err)
		}
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand ~: %w", err)
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}
