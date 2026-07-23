package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/linkanalabs/cli/internal/config"
)

func readConfigFile(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "lk", "config.yml"))
	if err != nil {
		return ""
	}
	return string(data)
}

func TestConfigShowDefaultSource(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(config.EnvBaseURL, "")

	var out, errOut bytes.Buffer
	if code := run([]string{"config", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	var v configView
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("json: %v (%q)", err, out.String())
	}
	if v.BaseURL != config.DefaultBaseURL {
		t.Errorf("base_url = %q, want %q", v.BaseURL, config.DefaultBaseURL)
	}
	if v.Source != "default" {
		t.Errorf("source = %q, want default", v.Source)
	}
}

func TestConfigShowEnvSource(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(config.EnvBaseURL, "http://from-env:3000")

	var out, errOut bytes.Buffer
	if code := run([]string{"config", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	var v configView
	_ = json.Unmarshal(out.Bytes(), &v)
	if v.BaseURL != "http://from-env:3000" || v.Source != "env" {
		t.Errorf("got base_url=%q source=%q, want env override", v.BaseURL, v.Source)
	}
}

func TestConfigSetURLWritesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv(config.EnvBaseURL, "")

	var out, errOut bytes.Buffer
	if code := run([]string{"config", "set-url", "https://app.linkana.com", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(readConfigFile(t, dir), "https://app.linkana.com") {
		t.Errorf("config file not written: %q", readConfigFile(t, dir))
	}
	var v configView
	_ = json.Unmarshal(out.Bytes(), &v)
	if v.BaseURL != "https://app.linkana.com" || v.Source != "file" {
		t.Errorf("got base_url=%q source=%q, want file", v.BaseURL, v.Source)
	}
}

func TestConfigSetURLRejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv(config.EnvBaseURL, "")

	for _, bad := range []string{"ftp://x", "not-a-url", "https://"} {
		var out, errOut bytes.Buffer
		code := run([]string{"config", "set-url", bad}, &out, &errOut)
		if code != 1 {
			t.Errorf("set-url %q: exit = %d, want 1", bad, code)
		}
		if readConfigFile(t, dir) != "" {
			t.Errorf("set-url %q wrote a file but should not have", bad)
		}
	}
}

func TestConfigSetURLWarnsWhenEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv(config.EnvBaseURL, "http://from-env:3000")

	var out, errOut bytes.Buffer
	if code := run([]string{"config", "set-url", "https://app.linkana.com"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(readConfigFile(t, dir), "https://app.linkana.com") {
		t.Error("file should still be written even when env overrides")
	}
	if !strings.Contains(errOut.String(), config.EnvBaseURL) {
		t.Errorf("stderr should warn that %s overrides the file, got %q", config.EnvBaseURL, errOut.String())
	}
}

func TestConfigViewStyled(t *testing.T) {
	v := configView{BaseURL: "https://app.linkana.com", Source: "file", Path: "/x/config.yml"}
	s := v.Styled()
	if !strings.Contains(s, "https://app.linkana.com") || !strings.Contains(s, "file") {
		t.Errorf("styled = %q", s)
	}
}
