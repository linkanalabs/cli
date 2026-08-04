//go:build windows

package update

import "os/exec"

// detach is a no-op on Windows: Homebrew does not exist there, so no code path
// reaches a spawned brew upgrade. It exists to keep SpawnUpgrade compiling for
// the windows/amd64 and windows/arm64 targets the release builds.
func detach(*exec.Cmd) {}
