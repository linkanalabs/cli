// Package state persists the CLI's own bookkeeping between runs.
//
// This is deliberately not internal/config: that file holds user configuration,
// is meant to be edited by hand, and is rewritten whole on every Save. Machine
// bookkeeping written on ordinary command exits does not belong in the same
// file as someone's base_url — that is how configuration gets lost. It follows
// the XDG state convention (~/.local/state), the same place gh and fizzy keep
// their update bookkeeping.
package state

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	dirName        = "lk"
	fileName       = "state.yml"
	upgradeLogName = "upgrade.log"
)

// State is what lk remembers across runs.
type State struct {
	// LastCheckAt throttles the automatic update check to once a day. It is
	// recorded even when no update is found, and claimed before the network
	// call, so an outage cannot turn into one request per command.
	//
	// Nothing else needs remembering: an upgrade is attempted at most once per
	// check, so this alone bounds the retries, and what brew actually did is in
	// the upgrade log rather than guessed at from a flag here.
	LastCheckAt time.Time `yaml:"last_check_at"`
}

// Seams so the failure branches of an atomic write are reachable from tests
// rather than left uncovered.
var (
	userHomeDir = os.UserHomeDir
	createTemp  = os.CreateTemp
	rename      = os.Rename
)

// Dir returns the state directory, honoring XDG_STATE_HOME.
func Dir() (string, error) {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, dirName), nil
	}
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating home directory: %w", err)
	}
	return filepath.Join(home, ".local", "state", dirName), nil
}

// path returns the full path to the state file.
func path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// UpgradeLogPath is where a background upgrade records what the package manager
// said. It lives here, next to Save, because Save is what creates the directory
// the log is written into.
func UpgradeLogPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, upgradeLogName), nil
}

// Load reads the state file. A missing file is not an error: the first run of
// a fresh install has no state.
func Load() (*State, error) {
	p, err := path()
	if err != nil {
		return nil, err
	}
	s := &State{}
	data, err := os.ReadFile(p)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(data, s); err != nil {
			return nil, fmt.Errorf("parsing state %s: %w", p, err)
		}
	case os.IsNotExist(err):
		// First run.
	default:
		return nil, fmt.Errorf("reading state %s: %w", p, err)
	}
	return s, nil
}

// Save writes the state atomically (temp + rename, the same approach the token
// store uses), so a concurrent reader never observes a half-written file.
func (s *State) Save() error {
	p, err := path()
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating state dir %s: %w", dir, err)
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("encoding state: %w", err)
	}

	// CreateTemp opens with mode 0600, which is the mode this file wants, so
	// no explicit Chmod is needed (see TestSavePermissions).
	tmp, err := createTemp(dir, ".state-*")
	if err != nil {
		return fmt.Errorf("creating temp state file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp state file: %w", err)
	}
	if err := rename(tmpName, p); err != nil {
		return fmt.Errorf("renaming state file: %w", err)
	}
	return nil
}
