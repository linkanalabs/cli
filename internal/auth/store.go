// Package auth stores the Linkana Personal Access Token (PAT), keyed by the
// backend origin (base URL). It mirrors fizzy-sdk's CredentialStore: the OS
// keychain is preferred, with an atomic, per-origin file fallback (temp file +
// rename, 0600). An environment override always wins.
package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	// EnvToken overrides any stored token when set (like fizzy's FIZZY_TOKEN).
	EnvToken = "LK_TOKEN"

	// EnvNoKeyring disables the OS keyring, forcing the file fallback. Used by
	// tests and headless environments.
	EnvNoKeyring = "LK_NO_KEYRING"

	// keyringService is the service name used for keychain entries.
	keyringService = "lk"

	dirName    = "lk"
	tokensSub  = "tokens"
	probeValue = "probe"
)

// Source identifies where a loaded token came from.
type Source string

// Token sources.
const (
	SourceNone    Source = "none"
	SourceEnv     Source = "env"
	SourceKeyring Source = "keychain"
	SourceFile    Source = "file"
)

// userHomeDir is a seam so tests can exercise the home-lookup failure path.
var userHomeDir = os.UserHomeDir

// keyringSet/Get/Delete are seams over the OS keyring so error branches are
// testable without a real keychain.
var (
	keyringSet    = keyring.Set
	keyringGet    = keyring.Get
	keyringDelete = keyring.Delete
)

// keyringAvailable reports whether the OS keyring should be used. It returns
// false when LK_NO_KEYRING is set, and otherwise probes the keyring with a
// throwaway write+delete.
func keyringAvailable() bool {
	if os.Getenv(EnvNoKeyring) != "" {
		return false
	}
	const probeKey = "__lk_probe__"
	if err := keyringSet(keyringService, probeKey, probeValue); err != nil {
		return false
	}
	_ = keyringDelete(keyringService, probeKey)
	return true
}

// loadStored reads the token for origin directly from the keychain/file store,
// bypassing the LK_TOKEN env override. This is the canonical "what is
// physically stored for this key?" lookup used by LoadImpersonation so that an
// explicit, sticky impersonation context is never masked by the ambient env var.
func loadStored(origin string) (string, Source, error) {
	if keyringAvailable() {
		tok, err := keyringGet(keyringService, origin)
		switch {
		case err == nil:
			return tok, SourceKeyring, nil
		case errors.Is(err, keyring.ErrNotFound):
			return "", SourceNone, nil
		default:
			return "", SourceNone, fmt.Errorf("reading keychain: %w", err)
		}
	}
	return loadFile(origin)
}

// Load returns the token for the given origin and where it came from. The
// LK_TOKEN env var wins over stored credentials. A missing token is not an
// error: it returns ("", SourceNone, nil).
func Load(origin string) (string, Source, error) {
	if tok := os.Getenv(EnvToken); tok != "" {
		return tok, SourceEnv, nil
	}
	return loadStored(origin)
}

// Save stores the token for the given origin.
func Save(origin, token string) error {
	if keyringAvailable() {
		if err := keyringSet(keyringService, origin, token); err != nil {
			return fmt.Errorf("writing keychain: %w", err)
		}
		return nil
	}
	return saveFile(origin, token)
}

// Delete removes the token for the given origin. A missing token is not an
// error.
func Delete(origin string) error {
	if keyringAvailable() {
		err := keyringDelete(keyringService, origin)
		if err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return fmt.Errorf("deleting keychain entry: %w", err)
		}
		return nil
	}
	return deleteFile(origin)
}

// --- file fallback ---

func tokensDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, dirName, tokensSub), nil
	}
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating home directory: %w", err)
	}
	return filepath.Join(home, ".config", dirName, tokensSub), nil
}

// fileNameForOrigin maps an origin to a stable, filesystem-safe file name.
func fileNameForOrigin(origin string) string {
	sum := sha256.Sum256([]byte(origin))
	return hex.EncodeToString(sum[:8]) + ".token"
}

func loadFile(origin string) (string, Source, error) {
	dir, err := tokensDir()
	if err != nil {
		return "", SourceNone, err
	}
	path := filepath.Join(dir, fileNameForOrigin(origin))
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		return strings.TrimSpace(string(data)), SourceFile, nil
	case os.IsNotExist(err):
		return "", SourceNone, nil
	default:
		return "", SourceNone, fmt.Errorf("reading token file %s: %w", path, err)
	}
}

func saveFile(origin, token string) error {
	dir, err := tokensDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating tokens dir %s: %w", dir, err)
	}
	path := filepath.Join(dir, fileNameForOrigin(origin))

	tmp, err := os.CreateTemp(dir, ".token-*")
	if err != nil {
		return fmt.Errorf("creating temp token file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("setting token file mode: %w", err)
	}
	if _, err := tmp.WriteString(token); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp token file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp token file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("renaming token file: %w", err)
	}
	return nil
}

func deleteFile(origin string) error {
	dir, err := tokensDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, fileNameForOrigin(origin))
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing token file %s: %w", path, err)
	}
	return nil
}
