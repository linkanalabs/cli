//go:build !windows

package update

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	// syscall.Getsid exists on Darwin and the BSDs but not on Linux, where the
	// CI runs. x/sys/unix wraps it for both and is already in the module graph;
	// this is a test-only import, so the shipped binary is unaffected.
	"golang.org/x/sys/unix"
)

// The point of spawning detached is that the upgrade outlives lk. Waiting on
// the child and reading its log proves it ran, not that it was detached — a
// regression that dropped Setsid would still pass that. This asserts the
// property the name claims: the child leads its own session, so lk exiting a
// moment later cannot take it down with it.
func TestSpawnUpgradeChildLeavesOurProcessGroup(t *testing.T) {
	script := filepath.Join(t.TempDir(), "brew")
	// Stay alive long enough to be inspected.
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubBrew(t, script, nil)

	cmd, err := SpawnUpgrade(filepath.Join(t.TempDir(), "upgrade.log"))
	if err != nil {
		t.Fatalf("SpawnUpgrade() error = %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// Sampling right after Start is not racy: Start waits for the exec attempt —
	// that is how it can report a non-executable brew, which
	// TestSpawnUpgradeStartFailure relies on — and Setsid runs in the child
	// before that exec. So a Start that returned nil means setsid already
	// happened. Confirmed over 300 consecutive runs.
	pid := cmd.Process.Pid
	group, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatalf("reading the child's process group: %v", err)
	}
	if group == syscall.Getpgrp() {
		t.Error("the upgrade runs in lk's own process group; it would die with lk")
	}
	if group != pid {
		t.Errorf("child process group = %d, want its own pid %d", group, pid)
	}
	// The session is the property that matters, and the one only Setsid gives:
	// swapping it for Setpgid would keep the group assertions green while the
	// child kept lk's controlling terminal.
	session, err := unix.Getsid(pid)
	if err != nil {
		t.Fatalf("reading the child's session: %v", err)
	}
	if session != pid {
		t.Errorf("child session = %d, want its own pid %d (Setsid was not applied)", session, pid)
	}
}
