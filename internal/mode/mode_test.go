package mode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsToRead(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	got, err := Load("http://localhost:3000")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != Read {
		t.Errorf("default = %q, want read", got)
	}
}

func TestSaveLoadPerOrigin(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Save("https://prod", Write); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Save("http://localhost:3000", Read); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if m, _ := Load("https://prod"); m != Write {
		t.Errorf("prod = %q, want write", m)
	}
	if m, _ := Load("http://localhost:3000"); m != Read {
		t.Errorf("local = %q, want read", m)
	}
}

func TestStatePathWithXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/xdg")
	path, err := statePath()
	if err != nil {
		t.Fatalf("statePath: %v", err)
	}
	if path != "/custom/xdg/lk/modes.json" {
		t.Errorf("statePath = %q, want /custom/xdg/lk/modes.json", path)
	}
}

func TestStatePathWithoutXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	oldUserHomeDir := userHomeDir
	t.Cleanup(func() { userHomeDir = oldUserHomeDir })
	userHomeDir = func() (string, error) {
		return "/home/user", nil
	}
	path, err := statePath()
	if err != nil {
		t.Fatalf("statePath: %v", err)
	}
	if path != "/home/user/.config/lk/modes.json" {
		t.Errorf("statePath = %q, want /home/user/.config/lk/modes.json", path)
	}
}

func TestStatePathHomeDirError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	oldUserHomeDir := userHomeDir
	t.Cleanup(func() { userHomeDir = oldUserHomeDir })
	userHomeDir = func() (string, error) {
		return "", os.ErrPermission
	}
	_, err := statePath()
	if err == nil {
		t.Fatal("statePath: expected error, got nil")
	}
}

func TestLoadAllUnmarshalError(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	modesDir := filepath.Join(configDir, "lk")
	if err := os.MkdirAll(modesDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	modesFile := filepath.Join(modesDir, "modes.json")
	if err := os.WriteFile(modesFile, []byte("{invalid json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := Load("https://test")
	if err == nil {
		t.Fatal("Load: expected parse error, got nil")
	}
}

func TestSaveMultipleOrigins(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := Save("origin1", Read); err != nil {
		t.Fatalf("Save origin1: %v", err)
	}
	if err := Save("origin2", Write); err != nil {
		t.Fatalf("Save origin2: %v", err)
	}
	if err := Save("origin3", Read); err != nil {
		t.Fatalf("Save origin3: %v", err)
	}

	if m, _ := Load("origin1"); m != Read {
		t.Errorf("origin1 = %q, want read", m)
	}
	if m, _ := Load("origin2"); m != Write {
		t.Errorf("origin2 = %q, want write", m)
	}
	if m, _ := Load("origin3"); m != Read {
		t.Errorf("origin3 = %q, want read", m)
	}
}

func TestSaveUpdate(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := Save("https://origin", Read); err != nil {
		t.Fatalf("Save Read: %v", err)
	}

	m, _ := Load("https://origin")
	if m != Read {
		t.Errorf("initial = %q, want read", m)
	}

	if err := Save("https://origin", Write); err != nil {
		t.Fatalf("Save Write: %v", err)
	}

	m, _ = Load("https://origin")
	if m != Write {
		t.Errorf("after update = %q, want write", m)
	}
}

func TestSaveCreatesDir(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	if err := Save("https://test", Write); err != nil {
		t.Fatalf("Save: %v", err)
	}

	modesFile := filepath.Join(configDir, "lk", "modes.json")
	if _, err := os.Stat(modesFile); err != nil {
		t.Fatalf("modes.json not created: %v", err)
	}
}

func TestSaveReadOnlyDirFails(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	// First save succeeds
	if err := Save("https://test", Read); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	// Make config dir read-only to force CreateTemp failure on the already-created dir
	lkDir := filepath.Join(configDir, "lk")
	if err := os.Chmod(lkDir, 0o555); err != nil {
		t.Fatalf("Chmod to ro: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(lkDir, 0o755) })

	err := Save("https://test2", Write)
	if err == nil {
		t.Fatal("Save to read-only dir: expected error, got nil")
	}
}

func TestLoadErrorReturnsReadWithErr(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	oldUserHomeDir := userHomeDir
	t.Cleanup(func() { userHomeDir = oldUserHomeDir })
	userHomeDir = func() (string, error) {
		return "", os.ErrPermission
	}

	m, err := Load("https://test")
	if err == nil {
		t.Fatal("Load: expected error, got nil")
	}
	if m != Read {
		t.Errorf("mode on error = %q, want read", m)
	}
}

func TestLoadAllReadError(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	// Create directory without file (file not present case)
	lkDir := filepath.Join(configDir, "lk")
	if err := os.MkdirAll(lkDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Then remove read permission to force read error
	if err := os.Chmod(lkDir, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(lkDir, 0o755) })

	_, err := Load("https://test")
	if err == nil {
		t.Fatal("Load: expected read error, got nil")
	}
}

func TestSaveAtomicFilePermissions(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	if err := Save("https://test", Write); err != nil {
		t.Fatalf("Save: %v", err)
	}

	modesFile := filepath.Join(configDir, "lk", "modes.json")
	info, err := os.Stat(modesFile)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	// Check that file has secure permissions (0o600)
	mode := info.Mode().Perm()
	if mode != 0o600 {
		t.Errorf("file permissions = %o, want 0600", mode)
	}
}

func TestSavePreservesExistingModes(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	// Save multiple origins
	origins := map[string]Mode{
		"https://prod":     Write,
		"http://localhost": Read,
		"https://staging":  Write,
	}

	for origin, mode := range origins {
		if err := Save(origin, mode); err != nil {
			t.Fatalf("Save %s: %v", origin, err)
		}
	}

	// Verify all are still there
	for origin, expectedMode := range origins {
		m, _ := Load(origin)
		if m != expectedMode {
			t.Errorf("Load %s = %q, want %q", origin, m, expectedMode)
		}
	}
}

func TestSaveCreateTempError(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	oldCreateTemp := createTemp
	t.Cleanup(func() { createTemp = oldCreateTemp })
	createTemp = func(_, _ string) (*os.File, error) {
		return nil, os.ErrPermission
	}

	err := Save("https://test", Write)
	if err == nil {
		t.Fatal("Save: expected temp creation error, got nil")
	}
}

func TestSaveRenameError(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	oldOsRename := osRename
	t.Cleanup(func() { osRename = oldOsRename })
	osRename = func(_, _ string) error {
		return os.ErrPermission
	}

	err := Save("https://test", Write)
	if err == nil {
		t.Fatal("Save: expected rename error, got nil")
	}
}

func TestLoadUnknownValueDefaultsToRead(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	// Write an unknown mode value directly to modes.json.
	lkDir := filepath.Join(configDir, "lk")
	if err := os.MkdirAll(lkDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	modesFile := filepath.Join(lkDir, "modes.json")
	if err := os.WriteFile(modesFile, []byte(`{"https://x":"banana"}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Load must fall back to Read for an unrecognised value.
	got, err := Load("https://x")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != Read {
		t.Errorf("Load for unknown value = %q, want read", got)
	}
}

func TestSaveWithLoadAllError(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	// Write garbage JSON so loadAll fails when Save calls it.
	lkDir := filepath.Join(configDir, "lk")
	if err := os.MkdirAll(lkDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	modesFile := filepath.Join(lkDir, "modes.json")
	if err := os.WriteFile(modesFile, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Save must propagate the loadAll parse error.
	err := Save("https://new", Write)
	if err == nil {
		t.Fatal("Save: expected error from loadAll, got nil")
	}
}

func TestSaveSaveStatePathError(t *testing.T) {
	// loadAll must succeed (XDG set, no existing file) then saveStatePathFn fails.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	oldSaveStatePathFn := saveStatePathFn
	t.Cleanup(func() { saveStatePathFn = oldSaveStatePathFn })
	saveStatePathFn = func() (string, error) {
		return "", os.ErrPermission
	}

	err := Save("https://x", Write)
	if err == nil {
		t.Fatal("Save: expected saveStatePathFn error, got nil")
	}
}

func TestSaveMkdirAllError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	oldMkdirAll := osMkdirAll
	t.Cleanup(func() { osMkdirAll = oldMkdirAll })
	osMkdirAll = func(_ string, _ os.FileMode) error {
		return os.ErrPermission
	}

	err := Save("https://test", Write)
	if err == nil {
		t.Fatal("Save: expected MkdirAll error, got nil")
	}
}

func TestSaveStatePath_HomeDirError(t *testing.T) {
	// Unset XDG so statePath falls through to userHomeDir.
	t.Setenv("XDG_CONFIG_HOME", "")

	oldUserHomeDir := userHomeDir
	t.Cleanup(func() { userHomeDir = oldUserHomeDir })
	userHomeDir = func() (string, error) {
		return "", os.ErrPermission
	}

	// Save must propagate the statePath error (which comes from loadAll's call to
	// statePath before the second statePath call inside Save).
	err := Save("https://x", Read)
	if err == nil {
		t.Fatal("Save: expected error when userHomeDir fails, got nil")
	}
}

func TestWriteAtomicallySuccess(t *testing.T) {
	configDir := t.TempDir()
	lkDir := filepath.Join(configDir, "lk")
	if err := os.MkdirAll(lkDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	path := filepath.Join(lkDir, "modes.json")
	data := []byte(`{"https://test": "write"}`)

	err := writeAtomically(lkDir, path, data)
	if err != nil {
		t.Fatalf("writeAtomically: %v", err)
	}

	// Verify file exists and has correct data
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != string(data) {
		t.Errorf("file content mismatch: got %q, want %q", content, data)
	}

	// Verify permissions
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestWriteAtomicallyChmodError(t *testing.T) {
	configDir := t.TempDir()
	lkDir := filepath.Join(configDir, "lk")
	if err := os.MkdirAll(lkDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	path := filepath.Join(lkDir, "modes.json")
	data := []byte(`{"https://test": "write"}`)

	oldFileChmod := fileChmod
	t.Cleanup(func() { fileChmod = oldFileChmod })
	fileChmod = func(_ *os.File, _ os.FileMode) error {
		return os.ErrPermission
	}

	err := writeAtomically(lkDir, path, data)
	if err == nil {
		t.Fatal("writeAtomically: expected chmod error, got nil")
	}
}

func TestWriteAtomicallyWriteError(t *testing.T) {
	configDir := t.TempDir()
	lkDir := filepath.Join(configDir, "lk")
	if err := os.MkdirAll(lkDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	path := filepath.Join(lkDir, "modes.json")
	data := []byte(`{"https://test": "write"}`)

	oldFileWrite := fileWrite
	t.Cleanup(func() { fileWrite = oldFileWrite })
	fileWrite = func(_ *os.File, _ []byte) (int, error) {
		return 0, os.ErrPermission
	}

	err := writeAtomically(lkDir, path, data)
	if err == nil {
		t.Fatal("writeAtomically: expected write error, got nil")
	}
}

func TestWriteAtomicallyCloseError(t *testing.T) {
	configDir := t.TempDir()
	lkDir := filepath.Join(configDir, "lk")
	if err := os.MkdirAll(lkDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	path := filepath.Join(lkDir, "modes.json")
	data := []byte(`{"https://test": "write"}`)

	oldFileClose := fileClose
	t.Cleanup(func() { fileClose = oldFileClose })
	fileClose = func(_ *os.File) error {
		return os.ErrPermission
	}

	err := writeAtomically(lkDir, path, data)
	if err == nil {
		t.Fatal("writeAtomically: expected close error, got nil")
	}
}
