package update

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// lookPath is a seam: tests point it at a stub so the upgrade path runs for
// real without touching the machine's Homebrew.
var lookPath = exec.LookPath

// brewArgs spells the upgrade out once. Nothing here ever runs `brew update`:
// it has no per-tap scope, so it would refresh every unrelated tap on the
// machine. Brew still auto-updates on its own schedule, exactly as it would for
// a hand-typed `brew upgrade`.
var brewArgs = []string{"upgrade", "--cask", "lk"}

func brewExecutable() (string, error) {
	path, err := lookPath("brew")
	if err != nil {
		return "", fmt.Errorf("brew is not on PATH: %w", err)
	}
	return path, nil
}

// RunUpgrade upgrades lk synchronously, streaming brew's own output to w.
// Callers pass stderr: stdout is the JSON contract and must stay clean.
//
// Replacing the binary is left entirely to brew. On a Workbrew fleet the
// Caskroom is owned by another user and brew is setuid root, so brew is not
// merely the polite choice — it is the only process that can write there.
func RunUpgrade(ctx context.Context, w io.Writer) error {
	path, err := brewExecutable()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, path, brewArgs...)
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("running `%s`: %w", BrewUpgradeCommand, err)
	}
	return nil
}

// SpawnUpgrade starts the same upgrade detached and returns as soon as it is
// running, so the automatic path never delays the process exit and never makes
// a calling agent wait. brew's output goes to logPath, because a background
// failure that leaves no trace cannot be diagnosed.
//
// The returned command is deliberately not waited on: the child is reparented
// when lk exits and finishes on its own. Tests wait on it to assert.
func SpawnUpgrade(logPath string) (*exec.Cmd, error) {
	path, err := brewExecutable()
	if err != nil {
		return nil, err
	}
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening the upgrade log %s: %w", logPath, err)
	}
	// The child inherits its own descriptor, so the parent's copy is done with
	// as soon as the process exists.
	defer func() { _ = log.Close() }()

	cmd := exec.Command(path, brewArgs...)
	cmd.Stdout = log
	cmd.Stderr = log
	detach(cmd)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting `%s`: %w", BrewUpgradeCommand, err)
	}
	return cmd, nil
}
