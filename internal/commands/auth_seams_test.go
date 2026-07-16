package commands

import (
	"bytes"
	"errors"
	"testing"

	"github.com/linkanalabs/cli/internal/auth"
)

// withSeam temporarily replaces a seam var and restores it.
func swapAuthLoad(t *testing.T, fn func(string) (string, auth.Source, error)) {
	t.Helper()
	orig := authLoad
	t.Cleanup(func() { authLoad = orig })
	authLoad = fn
}

func swapAuthSave(t *testing.T, fn func(string, string) error) {
	t.Helper()
	orig := authSave
	t.Cleanup(func() { authSave = orig })
	authSave = fn
}

func swapAuthDelete(t *testing.T, fn func(string) error) {
	t.Helper()
	orig := authDelete
	t.Cleanup(func() { authDelete = orig })
	authDelete = fn
}

func TestAuthLoginSaveError(t *testing.T) {
	authEnv(t)
	swapAuthSave(t, func(string, string) error { return errors.New("disk full") })
	var out, errOut bytes.Buffer
	code := run([]string{"auth", "login", "--token", "lkn_abc_def"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !bytes.Contains(errOut.Bytes(), []byte("saving token")) {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestAuthStatusLoadError(t *testing.T) {
	authEnv(t)
	swapAuthLoad(t, func(string) (string, auth.Source, error) {
		return "", auth.SourceNone, errors.New("keychain locked")
	})
	var out, errOut bytes.Buffer
	code := run([]string{"auth", "status"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestAuthLogoutError(t *testing.T) {
	authEnv(t)
	swapAuthDelete(t, func(string) error { return errors.New("cannot delete") })
	var out, errOut bytes.Buffer
	code := run([]string{"auth", "logout"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !bytes.Contains(errOut.Bytes(), []byte("deleting token")) {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestPromptTokenScannerError(t *testing.T) {
	// A reader that always errors exercises the scanner-error branch.
	_, err := promptToken(errReader{}, &bytes.Buffer{}, "http://localhost:3000")
	if err == nil {
		t.Error("expected scanner error")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }
