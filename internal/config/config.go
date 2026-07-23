// Package config loads and persists the lk CLI configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultBaseURL is used when no base URL is configured. It points at
	// production so a fresh install (e.g. via Homebrew) talks to the real
	// backend by default; local development overrides it with LK_API_URL
	// (see `make dev`) or a config file.
	DefaultBaseURL = "https://app.linkana.com"

	// EnvBaseURL overrides the configured base URL when set.
	EnvBaseURL = "LK_API_URL"

	dirName  = "lk"
	fileName = "config.yml"
)

// Config holds the CLI configuration.
type Config struct {
	BaseURL string `yaml:"base_url"`
}

// userHomeDir is a seam so tests can exercise the home-lookup failure path.
var userHomeDir = os.UserHomeDir

// Dir returns the config directory, honoring XDG_CONFIG_HOME.
func Dir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, dirName), nil
	}
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating home directory: %w", err)
	}
	return filepath.Join(home, ".config", dirName), nil
}

// Path returns the full path to the config file.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// Load reads the config file (if present), applies the LK_API_URL override,
// and falls back to defaults. A missing file is not an error.
func Load() (*Config, error) {
	cfg := &Config{}

	path, err := Path()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing config %s: %w", path, err)
		}
	case os.IsNotExist(err):
		// No file yet — defaults apply.
	default:
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	if env := os.Getenv(EnvBaseURL); env != "" {
		cfg.BaseURL = env
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	return cfg, nil
}

// FileBaseURL returns the base_url set in the config file only, ignoring the
// LK_API_URL override and the default. It returns "" when there is no file or
// the file does not set a base_url. A malformed file surfaces as an error.
func FileBaseURL() (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading config %s: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return "", fmt.Errorf("parsing config %s: %w", path, err)
	}
	return c.BaseURL, nil
}

// Save writes the config to disk, creating the directory if needed.
func (c *Config) Save() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config dir %s: %w", dir, err)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	path := filepath.Join(dir, fileName)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}
	return nil
}
