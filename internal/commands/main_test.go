package commands

import (
	"os"
	"testing"
)

// TestMain keeps every test away from the developer's real manifest copy and
// from the network: run() would otherwise ask the backend configured on the
// machine for its manifest. Tests that exercise the remote manifest unset the
// variable and point the state directory at their own temp dir.
func TestMain(m *testing.M) {
	stateDir, err := os.MkdirTemp("", "lk-commands-state-*")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("XDG_STATE_HOME", stateDir)
	_ = os.Setenv(EnvNoRemoteManifest, "1")
	code := m.Run()
	_ = os.RemoveAll(stateDir)
	os.Exit(code)
}
