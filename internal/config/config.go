package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	configDir  = ".civcat"
	configFile = "config.json"
)

type Config struct {
	ComfyUIPath      string `json:"comfyui_path"`
	APIKey           string `json:"api_key,omitempty"`
	HFToken          string `json:"hf_token,omitempty"`
	ParallelDownload bool   `json:"parallel_download,omitempty"`
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, configDir, configFile), nil
}

func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, configDir), nil
}

func Load() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Environment variable overrides config file for API key.
	if envKey := os.Getenv("CIVITAI_API_KEY"); envKey != "" {
		cfg.APIKey = envKey
	}
	if hfToken := os.Getenv("HF_TOKEN"); hfToken != "" {
		cfg.HFToken = hfToken
	}

	return &cfg, nil
}

func (c *Config) Save() error {
	path, err := configPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	return nil
}

func (c *Config) IsConfigured() bool {
	return c.ComfyUIPath != ""
}

// GetAPIKey returns the API key from env var or config.
func (c *Config) GetAPIKey() string {
	if envKey := os.Getenv("CIVITAI_API_KEY"); envKey != "" {
		return envKey
	}
	return c.APIKey
}

// GetHFToken returns the Hugging Face token from env var or config.
func (c *Config) GetHFToken() string {
	if token := os.Getenv("HF_TOKEN"); token != "" {
		return token
	}
	return c.HFToken
}
