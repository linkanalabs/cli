package update

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// fakeBrew writes a script that records how it was called and exits with the
// given code, so the upgrade path is exercised for real without touching the
// machine's Homebrew.
func fakeBrew(t *testing.T, exitCode int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Homebrew is unix-only, and so is this path")
	}
	path := filepath.Join(t.TempDir(), "brew")
	script := "#!/bin/sh\necho \"called: $@\"\nexit " + strconv.Itoa(exitCode) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func stubBrew(t *testing.T, path string, err error) {
	t.Helper()
	prev := lookPath
	lookPath = func(string) (string, error) { return path, err }
	t.Cleanup(func() { lookPath = prev })
}

// The upgrade must be scoped to lk alone: a bare `brew update` would refresh
// every unrelated tap on the machine.
func TestRunUpgradeInvokesOnlyLk(t *testing.T) {
	stubBrew(t, fakeBrew(t, 0), nil)

	var out bytes.Buffer
	if err := RunUpgrade(context.Background(), &out); err != nil {
		t.Fatalf("RunUpgrade() error = %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "upgrade --cask lk") {
		t.Errorf("brew called as %q, want `upgrade --cask lk`", strings.TrimSpace(got))
	}
	if strings.Contains(got, "update") {
		t.Errorf("brew was asked to update globally: %q", strings.TrimSpace(got))
	}
}

func TestRunUpgradeFailure(t *testing.T) {
	stubBrew(t, fakeBrew(t, 1), nil)

	var out bytes.Buffer
	if err := RunUpgrade(context.Background(), &out); err == nil {
		t.Fatal("expected an error when brew exits non-zero")
	}
}

func TestRunUpgradeBrewMissing(t *testing.T) {
	stubBrew(t, "", errors.New("not found"))

	err := RunUpgrade(context.Background(), &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected an error when brew is not on PATH")
	}
	if !strings.Contains(err.Error(), "brew") {
		t.Errorf("error should name brew, got %v", err)
	}
}

// The automatic path must not hold the process open: lk starts brew and exits,
// so the upgrade outlives it and the agent is never made to wait.
func TestSpawnUpgradeDetaches(t *testing.T) {
	stubBrew(t, fakeBrew(t, 0), nil)
	logPath := filepath.Join(t.TempDir(), "upgrade.log")

	cmd, err := SpawnUpgrade(logPath)
	if err != nil {
		t.Fatalf("SpawnUpgrade() error = %v", err)
	}
	if cmd.Process == nil {
		t.Fatal("no process was started")
	}
	// Production never waits; the test does, to assert on the log.
	_ = cmd.Wait()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading the upgrade log: %v", err)
	}
	if !strings.Contains(string(data), "upgrade --cask lk") {
		t.Errorf("upgrade log = %q", string(data))
	}
}

// A background failure has to leave a trace, or it is undiagnosable.
func TestSpawnUpgradeLogsFailure(t *testing.T) {
	stubBrew(t, fakeBrew(t, 1), nil)
	logPath := filepath.Join(t.TempDir(), "upgrade.log")

	cmd, err := SpawnUpgrade(logPath)
	if err != nil {
		t.Fatalf("SpawnUpgrade() error = %v", err)
	}
	if werr := cmd.Wait(); werr == nil {
		t.Fatal("expected the background upgrade to report failure")
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Errorf("no log was written: %v", err)
	}
}

func TestSpawnUpgradeBrewMissing(t *testing.T) {
	stubBrew(t, "", errors.New("not found"))

	if _, err := SpawnUpgrade(filepath.Join(t.TempDir(), "upgrade.log")); err == nil {
		t.Fatal("expected an error when brew is not on PATH")
	}
}

func TestSpawnUpgradeUnwritableLog(t *testing.T) {
	stubBrew(t, fakeBrew(t, 0), nil)

	// A directory that does not exist cannot hold the log.
	logPath := filepath.Join(t.TempDir(), "absent", "upgrade.log")
	if _, err := SpawnUpgrade(logPath); err == nil {
		t.Fatal("expected an error when the log cannot be opened")
	}
}
