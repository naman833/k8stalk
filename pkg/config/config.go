package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

// Config represents the k8stalk configuration file.
type Config struct {
	DefaultBackend string           `yaml:"default_backend"`
	Providers      []ProviderConfig `yaml:"providers"`
}

// ProviderConfig holds configuration for a single LLM provider backend.
type ProviderConfig struct {
	Backend   string `yaml:"backend"`
	Model     string `yaml:"model,omitempty"`
	BaseURL   string `yaml:"base_url,omitempty"`
	Region    string `yaml:"region,omitempty"`
	APIKeyEnv string `yaml:"api_key_env,omitempty"` // env var name, never the raw key
}

// ConfigDir returns the config directory path.
func ConfigDir() string {
	switch runtime.GOOS {
	case "windows":
		if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
			return filepath.Join(dir, "k8stalk")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "AppData", "Local", "k8stalk")
	default:
		if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
			return filepath.Join(dir, "k8stalk")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "k8stalk")
	}
}

// ConfigPath returns the path to the config.yaml file.
func ConfigPath() string {
	return filepath.Join(ConfigDir(), "config.yaml")
}

// HistoryDBPath returns the path to the SQLite history database.
func HistoryDBPath() string {
	return filepath.Join(ConfigDir(), "history.db")
}

// Load reads the config from disk.
func Load() (*Config, error) {
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return &cfg, nil
}

// Save writes the config to disk, creating the directory if needed.
func Save(cfg *Config) error {
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(ConfigPath(), data, 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}
