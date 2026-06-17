package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const testOrigin = "http://localhost:3000"

// keyringOff configures the store to skip the OS keyring and use the file
// fallback rooted at a temp dir, and clears the env override.
func keyringOff(t *testing.T) {
	t.Helper()
	t.Setenv(EnvNoKeyring, "1")
	t.Setenv(EnvToken, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func TestSaveLoadRoundTripFile(t *testing.T) {
	keyringOff(t)

	if err := Save(testOrigin, "lkn_abc_def"); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	tok, src, err := Load(testOrigin)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if tok != "lkn_abc_def" {
		t.Errorf("token = %q", tok)
	}
	if src != SourceFile {
		t.Errorf("source = %q, want %q", src, SourceFile)
	}
}

func TestLoadMissingFile(t *testing.T) {
	keyringOff(t)

	tok, src, err := Load(testOrigin)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if tok != "" {
		t.Errorf("token = %q, want empty", tok)
	}
	if src != SourceNone {
		t.Errorf("source = %q, want %q", src, SourceNone)
	}
}

func TestEnvOverrideWins(t *testing.T) {
	keyringOff(t)
	if err := Save(testOrigin, "lkn_file_token"); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	t.Setenv(EnvToken, "lkn_env_token")

	tok, src, err := Load(testOrigin)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if tok != "lkn_env_token" {
		t.Errorf("token = %q, want env token", tok)
	}
	if src != SourceEnv {
		t.Errorf("source = %q, want %q", src, SourceEnv)
	}
}

func TestDeleteFile(t *testing.T) {
	keyringOff(t)
	if err := Save(testOrigin, "lkn_abc_def"); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if err := Delete(testOrigin); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	tok, src, err := Load(testOrigin)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if tok != "" || src != SourceNone {
		t.Errorf("after delete token=%q src=%q", tok, src)
	}
}

func TestDeleteMissingIsNoError(t *testing.T) {
	keyringOff(t)
	if err := Delete(testOrigin); err != nil {
		t.Errorf("Delete() on missing should be nil, got %v", err)
	}
}

func TestKeyringAvailableProbe(t *testing.T) {
	t.Setenv(EnvNoKeyring, "1")
	if keyringAvailable() {
		t.Error("keyringAvailable() should be false when LK_NO_KEYRING set")
	}
	t.Setenv(EnvNoKeyring, "")
	// Without the env, the probe depends on the platform; just ensure it runs.
	_ = keyringAvailable()
}

func TestFilePermissions(t *testing.T) {
	keyringOff(t)
	if err := Save(testOrigin, "lkn_abc_def"); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	dir, err := tokensDir()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, fileNameForOrigin(testOrigin)))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perm = %o, want 600", perm)
	}
}

func TestSaveFileMkdirError(t *testing.T) {
	keyringOff(t)
	// Point XDG at a path whose parent is a regular file → MkdirAll fails.
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", file)
	if err := Save(testOrigin, "lkn_abc_def"); err == nil {
		t.Error("expected Save error when dir cannot be created")
	}
}

func TestLoadFileReadError(t *testing.T) {
	keyringOff(t)
	dir, err := tokensDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Create a directory where the token file is expected → ReadFile errors.
	if err := os.MkdirAll(filepath.Join(dir, fileNameForOrigin(testOrigin)), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(testOrigin); err == nil {
		t.Error("expected Load read error")
	}
}

func TestTokensDirHomeError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	orig := userHomeDir
	defer func() { userHomeDir = orig }()
	userHomeDir = func() (string, error) { return "", errors.New("no home") }
	if _, err := tokensDir(); err == nil {
		t.Error("expected tokensDir error when home lookup fails")
	}
}

func TestSaveFileRenameError(t *testing.T) {
	keyringOff(t)
	dir, err := tokensDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Make the destination a non-empty directory so rename(2) fails.
	dest := filepath.Join(dir, fileNameForOrigin(testOrigin))
	if err := os.MkdirAll(filepath.Join(dest, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Save(testOrigin, "lkn_abc_def"); err == nil {
		t.Error("expected Save rename error when destination is a non-empty dir")
	}
}

func TestDeleteFileError(t *testing.T) {
	keyringOff(t)
	dir, err := tokensDir()
	if err != nil {
		t.Fatal(err)
	}
	// Make the token path a non-empty directory so os.Remove fails.
	dest := filepath.Join(dir, fileNameForOrigin(testOrigin))
	if err := os.MkdirAll(filepath.Join(dest, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Delete(testOrigin); err == nil {
		t.Error("expected Delete error when path is a non-empty dir")
	}
}

func TestLoadFileHomeError(t *testing.T) {
	t.Setenv(EnvNoKeyring, "1")
	t.Setenv(EnvToken, "")
	t.Setenv("XDG_CONFIG_HOME", "")
	orig := userHomeDir
	defer func() { userHomeDir = orig }()
	userHomeDir = func() (string, error) { return "", errors.New("no home") }
	if _, _, err := Load(testOrigin); err == nil {
		t.Error("expected Load error when home lookup fails")
	}
}

func TestDeleteFileHomeError(t *testing.T) {
	t.Setenv(EnvNoKeyring, "1")
	t.Setenv(EnvToken, "")
	t.Setenv("XDG_CONFIG_HOME", "")
	orig := userHomeDir
	defer func() { userHomeDir = orig }()
	userHomeDir = func() (string, error) { return "", errors.New("no home") }
	if err := Delete(testOrigin); err == nil {
		t.Error("expected Delete error when home lookup fails")
	}
}

func TestFileNameForOriginStable(t *testing.T) {
	a := fileNameForOrigin("http://localhost:3000")
	b := fileNameForOrigin("http://localhost:3000")
	c := fileNameForOrigin("https://app.linkana.com")
	if a != b {
		t.Error("fileNameForOrigin not stable for same origin")
	}
	if a == c {
		t.Error("fileNameForOrigin collided for different origins")
	}
}
