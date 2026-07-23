package commands

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files (SURFACE.txt)")

// TestSurfaceGolden regenerates the full command surface (manual + dynamic,
// from the real embedded manifest) and compares it against SURFACE.txt. Run
// `go test ./internal/commands -run TestSurfaceGolden -update` to refresh the
// golden after an intentional surface change.
func TestSurfaceGolden(t *testing.T) {
	got := surface(newRootCmd())
	path := filepath.Join("..", "..", "SURFACE.txt")

	if *updateGolden {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("updating golden: %v", err)
		}
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden: %v (run with -update to create it)", err)
	}
	if got != string(want) {
		t.Errorf("command surface drifted from SURFACE.txt.\n"+
			"If intentional, refresh with: go test ./internal/commands -run TestSurfaceGolden -update\n"+
			"--- got ---\n%s--- want ---\n%s", got, want)
	}
}

func TestSurfaceIncludesDynamicCommands(t *testing.T) {
	got := surface(newRootCmd())
	for _, want := range []string{
		"lk identity show",
		"lk settings email-message list",
		"lk settings email-message show",
		"lk settings email-message update",
		"lk supplier list",
		"lk whoami",
	} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("surface missing %q:\n%s", want, got)
		}
	}
}
