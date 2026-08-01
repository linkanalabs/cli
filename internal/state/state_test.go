package state

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDirHonorsXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/custom/state")

	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir() error = %v", err)
	}
	if want := filepath.Join("/custom/state", "lk"); got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
}

// The XDG default for state is ~/.local/state, not ~/.config: state is
// machine-local bookkeeping, not user configuration.
func TestDirFallsBackToLocalState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	prev := userHomeDir
	userHomeDir = func() (string, error) { return "/home/tester", nil }
	t.Cleanup(func() { userHomeDir = prev })

	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir() error = %v", err)
	}
	if want := filepath.Join("/home/tester", ".local", "state", "lk"); got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
}

func TestDirHomeLookupFailure(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	prev := userHomeDir
	userHomeDir = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { userHomeDir = prev })

	if _, err := Dir(); err == nil {
		t.Fatal("expected an error when the home directory cannot be resolved")
	}
	if _, err := path(); err == nil {
		t.Fatal("expected path to propagate the home lookup failure")
	}
	if _, err := UpgradeLogPath(); err == nil {
		t.Fatal("expected UpgradeLogPath to propagate the home lookup failure")
	}
	if _, err := Load(); err == nil {
		t.Fatal("expected Load to propagate the home lookup failure")
	}
	s := &State{}
	if err := s.Save(); err == nil {
		t.Fatal("expected Save to propagate the home lookup failure")
	}
}

// A first run has no state file, and that is the normal case — never an error.
func TestLoadMissingFile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !got.LastCheckAt.IsZero() {
		t.Errorf("expected a zero state, got %+v", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	checked := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	if err := (&State{LastCheckAt: checked}).Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !got.LastCheckAt.Equal(checked) {
		t.Errorf("LastCheckAt = %v, want %v", got.LastCheckAt, checked)
	}
}

// State records what the machine has been doing; it stays out of reach of
// other users, like the token store already does.
func TestSavePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits")
	}
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)

	if err := (&State{LastCheckAt: time.Now()}).Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	dir := filepath.Join(root, "lk")
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir mode = %o, want 700", perm)
	}
	fi, err := os.Stat(filepath.Join(dir, "state.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600", perm)
	}
}

// Two lk processes can exit at the same time; a half-written state file must
// never be observable, and no temp files may pile up.
func TestSaveIsAtomic(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)

	for i := 0; i < 3; i++ {
		if err := (&State{LastCheckAt: time.Now()}).Save(); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
	}

	entries, err := os.ReadDir(filepath.Join(root, "lk"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "state.yml" {
			t.Errorf("leftover file in the state dir: %q", e.Name())
		}
	}
}

func TestLoadMalformed(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	dir := filepath.Join(root, "lk")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.yml"), []byte("\tnot: [valid"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(); err == nil {
		t.Fatal("expected an error for a malformed state file")
	}
}

func TestPaths(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/s")

	got, err := path()
	if err != nil {
		t.Fatalf("path() error = %v", err)
	}
	if !strings.HasSuffix(got, filepath.Join("lk", "state.yml")) {
		t.Errorf("path() = %q", got)
	}

	// The log sits beside the state file, so the directory Save creates is the
	// one the log is written into.
	log, err := UpgradeLogPath()
	if err != nil {
		t.Fatalf("UpgradeLogPath() error = %v", err)
	}
	if !strings.HasSuffix(log, filepath.Join("lk", "upgrade.log")) {
		t.Errorf("UpgradeLogPath() = %q", log)
	}
}

// Save must fail loudly rather than silently lose the throttle when the state
// directory cannot be created.
func TestSaveUnwritableDir(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs unix permissions and a non-root user")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })
	t.Setenv("XDG_STATE_HOME", root)

	if err := (&State{}).Save(); err == nil {
		t.Fatal("expected an error when the state dir cannot be created")
	}
}

// --- atomic write failure branches ---

func TestSaveTempFileFailure(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	prev := createTemp
	createTemp = func(string, string) (*os.File, error) { return nil, errors.New("no temp files") }
	t.Cleanup(func() { createTemp = prev })

	if err := (&State{}).Save(); err == nil {
		t.Fatal("expected an error when the temp file cannot be created")
	}
}

func TestSaveWriteFailure(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	prev := createTemp
	createTemp = func(dir, pattern string) (*os.File, error) {
		f, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return nil, err
		}
		// Already closed: the write that follows cannot succeed.
		_ = f.Close()
		return f, nil
	}
	t.Cleanup(func() { createTemp = prev })

	if err := (&State{}).Save(); err == nil {
		t.Fatal("expected an error when the temp file cannot be written")
	}
}

// A failed rename must surface: silently losing the throttle would mean
// checking on every single command.
func TestSaveRenameFailure(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	prev := rename
	rename = func(string, string) error { return errors.New("cross-device link") }
	t.Cleanup(func() { rename = prev })

	if err := (&State{}).Save(); err == nil {
		t.Fatal("expected an error when the rename fails")
	}
}

// An unreadable state file is an error, not an empty state: treating it as
// empty would silently reset the throttle on every run.
func TestLoadUnreadable(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	// A directory where the file should be: reading it fails with something
	// other than "not exist".
	if err := os.MkdirAll(filepath.Join(root, "lk", "state.yml"), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(); err == nil {
		t.Fatal("expected an error when the state file cannot be read")
	}
}
