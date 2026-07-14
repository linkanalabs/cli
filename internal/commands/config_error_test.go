package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// badConfigEnv points XDG at a dir containing a malformed config.yml so
// config.Load returns an error, exercising the config-error branches.
func badConfigEnv(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	lkDir := filepath.Join(dir, "lk")
	if err := os.MkdirAll(lkDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lkDir, "config.yml"), []byte("base_url: [unterminated"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("LK_NO_KEYRING", "1")
	t.Setenv("LK_TOKEN", "")
	// LK_API_URL must be unset so config parsing is actually reached.
	t.Setenv("LK_API_URL", "")
}

func TestAuthLoginConfigError(t *testing.T) {
	badConfigEnv(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"auth", "login", "--token", "lkn_a_b"}, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestAuthStatusConfigError(t *testing.T) {
	badConfigEnv(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"auth", "status"}, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestAuthLogoutConfigError(t *testing.T) {
	badConfigEnv(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"auth", "logout"}, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestWhoamiConfigError(t *testing.T) {
	badConfigEnv(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"whoami"}, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestModeConfigError(t *testing.T) {
	badConfigEnv(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"mode"}, &out, &errOut); code != 1 {
		t.Fatalf("mode: exit = %d, want 1", code)
	}
}

func TestModeWriteConfigError(t *testing.T) {
	badConfigEnv(t)
	prev := isStdinTTY
	defer func() { isStdinTTY = prev }()
	isStdinTTY = func() bool { return true }
	var out, errOut bytes.Buffer
	if code := runWith(strings.NewReader("write\n"), []string{"mode", "write"}, &out, &errOut); code != 1 {
		t.Fatalf("mode write config error: exit = %d, want 1", code)
	}
}

func TestModeReadConfigError(t *testing.T) {
	badConfigEnv(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"mode", "read"}, &out, &errOut); code != 1 {
		t.Fatalf("mode read config error: exit = %d, want 1", code)
	}
}
