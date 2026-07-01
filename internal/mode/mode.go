// Package mode persists the lk read/write mode per backend origin. read is the
// default; write must be explicitly enabled and gates mutating requests.
package mode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Mode is the read/write permission level for a backend origin.
type Mode string

const (
	// Read is the default read-only mode.
	Read Mode = "read"
	// Write enables mutation.
	Write Mode = "write"
)

var (
	userHomeDir     = os.UserHomeDir
	createTemp      = os.CreateTemp
	osRename        = os.Rename
	osRemove        = os.Remove
	fileChmod       = (*os.File).Chmod
	fileWrite       = (*os.File).Write
	fileClose       = (*os.File).Close
	osMkdirAll      = os.MkdirAll // seam so tests can force the MkdirAll error in Save
	saveStatePathFn = statePath   // seam for the statePath call inside Save
)

func statePath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "lk", "modes.json"), nil
	}
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating home directory: %w", err)
	}
	return filepath.Join(home, ".config", "lk", "modes.json"), nil
}

func loadAll() (map[string]Mode, error) {
	path, err := statePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		m := map[string]Mode{}
		if err := json.Unmarshal(data, &m); err != nil {
			// A corrupt modes.json must not brick the CLI. Fail safe to the
			// restrictive default (read) by treating it as empty; a later Save
			// rewrites a clean file. The gate fails closed-to-read, never
			// closed-to-error.
			return map[string]Mode{}, nil
		}
		return m, nil
	case os.IsNotExist(err):
		return map[string]Mode{}, nil
	default:
		return nil, fmt.Errorf("reading modes %s: %w", path, err)
	}
}

// Load returns the mode for origin, defaulting to Read when unset.
func Load(origin string) (Mode, error) {
	all, err := loadAll()
	if err != nil {
		return Read, err
	}
	if m, ok := all[origin]; ok && m == Write {
		return Write, nil
	}
	return Read, nil
}

// Save persists the mode for origin atomically.
func Save(origin string, m Mode) error {
	all, err := loadAll()
	if err != nil {
		return err
	}
	all[origin] = m
	path, err := saveStatePathFn()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := osMkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating modes dir %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding modes: %w", err)
	}
	return writeAtomically(dir, path, data)
}

func writeAtomically(dir string, path string, data []byte) error {
	tmp, err := createTemp(dir, ".modes-*")
	if err != nil {
		return fmt.Errorf("creating temp modes file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = osRemove(tmpName) }()
	if err := fileChmod(tmp, 0o600); err != nil {
		_ = fileClose(tmp)
		return fmt.Errorf("setting modes file mode: %w", err)
	}
	if _, err := fileWrite(tmp, data); err != nil {
		_ = fileClose(tmp)
		return fmt.Errorf("writing temp modes file: %w", err)
	}
	if err := fileClose(tmp); err != nil {
		return fmt.Errorf("closing temp modes file: %w", err)
	}
	if err := osRename(tmpName, path); err != nil {
		return fmt.Errorf("renaming modes file: %w", err)
	}
	return nil
}
