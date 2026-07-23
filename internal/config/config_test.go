package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirHonorsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir() error: %v", err)
	}
	if want := filepath.Join("/tmp/xdg", "lk"); got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
}

func TestDirFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/tmp/home")
	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir() error: %v", err)
	}
	if want := filepath.Join("/tmp/home", ".config", "lk"); got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
}

func TestPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	got, err := Path()
	if err != nil {
		t.Fatalf("Path() error: %v", err)
	}
	if want := filepath.Join("/tmp/xdg", "lk", "config.yml"); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestLoadMissingFileUsesDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(EnvBaseURL, "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.BaseURL != DefaultBaseURL {
		t.Errorf("BaseURL = %q, want default %q", cfg.BaseURL, DefaultBaseURL)
	}
}

func TestLoadReadsFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv(EnvBaseURL, "")
	writeConfig(t, dir, "base_url: https://api.example.com\n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.BaseURL != "https://api.example.com" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
}

func TestLoadEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeConfig(t, dir, "base_url: https://file.example.com\n")
	t.Setenv(EnvBaseURL, "https://env.example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.BaseURL != "https://env.example.com" {
		t.Errorf("BaseURL = %q, want env override", cfg.BaseURL)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeConfig(t, dir, "base_url: [not a string\n")
	if _, err := Load(); err == nil {
		t.Fatal("Load() expected error for invalid YAML")
	}
}

func TestLoadReadError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	// Make the config path a directory so ReadFile fails with a non-NotExist error.
	if err := os.MkdirAll(filepath.Join(dir, "lk", "config.yml"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("Load() expected error when config path is a directory")
	}
}

func TestSaveThenLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv(EnvBaseURL, "")

	cfg := &Config{BaseURL: "https://saved.example.com"}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.BaseURL != cfg.BaseURL {
		t.Errorf("round-trip BaseURL = %q, want %q", loaded.BaseURL, cfg.BaseURL)
	}
}

func TestDirHomeError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	orig := userHomeDir
	defer func() { userHomeDir = orig }()
	userHomeDir = func() (string, error) { return "", os.ErrNotExist }

	if _, err := Dir(); err == nil {
		t.Error("Dir() expected error when home lookup fails")
	}
	if _, err := Path(); err == nil {
		t.Error("Path() expected error when Dir fails")
	}
}

func TestSaveWriteFileError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	// Make the config file path a directory so WriteFile fails.
	if err := os.MkdirAll(filepath.Join(dir, "lk", "config.yml"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{BaseURL: "https://x"}
	if err := cfg.Save(); err == nil {
		t.Fatal("Save() expected error when file path is a directory")
	}
}

func TestSaveMkdirError(t *testing.T) {
	dir := t.TempDir()
	// Put a file where the config dir parent should be a directory.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(blocker, "nested"))

	cfg := &Config{BaseURL: "https://x"}
	if err := cfg.Save(); err == nil {
		t.Fatal("Save() expected error when dir cannot be created")
	}
}

func TestFileBaseURL(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		got, err := FileBaseURL()
		if err != nil || got != "" {
			t.Errorf("got %q, %v; want empty, nil", got, err)
		}
	})
	t.Run("file with base_url ignores env override", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", dir)
		t.Setenv(EnvBaseURL, "https://env.example.com")
		writeConfig(t, dir, "base_url: https://file.example.com\n")
		got, err := FileBaseURL()
		if err != nil || got != "https://file.example.com" {
			t.Errorf("got %q, %v; want file value, nil", got, err)
		}
	})
	t.Run("malformed file errors", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", dir)
		writeConfig(t, dir, "base_url: [unclosed\n")
		if _, err := FileBaseURL(); err == nil {
			t.Error("expected parse error")
		}
	})
}

func writeConfig(t *testing.T, xdgDir, content string) {
	t.Helper()
	cfgDir := filepath.Join(xdgDir, "lk")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
