//go:build !windows

package update

import (
	"os/exec"
	"syscall"
)

// detach puts the upgrade in its own session, so it is not in lk's process
// group and survives lk exiting a moment later.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
