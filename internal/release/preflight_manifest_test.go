package release_test

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The --manifest-only gate compares the vendored manifest against the one on
// linkana's main. `gh` is stubbed so the test never touches the network: the stub
// prints the base64 payload the real `gh api ... --jq .content` would print.
func runManifestGate(t *testing.T, remote map[string]any) (string, int) {
	t.Helper()

	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq não instalado — o gate do preflight depende dele")
	}

	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}

	payload, err := json.Marshal(remote)
	if err != nil {
		t.Fatalf("marshalling remote manifest: %v", err)
	}

	stubDir := t.TempDir()
	stub := "#!/usr/bin/env bash\nprintf '%s' " +
		shellQuote(base64.StdEncoding.EncodeToString(payload)) + "\n"
	if err := os.WriteFile(filepath.Join(stubDir, "gh"), []byte(stub), 0o755); err != nil {
		t.Fatalf("writing gh stub: %v", err)
	}

	cmd := exec.Command("./scripts/release-preflight.sh", "--manifest-only")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	out, err := cmd.CombinedOutput()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("running preflight: %v (output: %s)", err, out)
	}
	return string(out), code
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func vendoredManifest(t *testing.T) map[string]any {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "manifest", "cli-manifest.json"))
	if err != nil {
		t.Fatalf("reading vendored manifest: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parsing vendored manifest: %v", err)
	}
	return m
}

func TestManifestGatePassesWhenVendoredMatchesRemote(t *testing.T) {
	out, code := runManifestGate(t, vendoredManifest(t))

	if code != 0 {
		t.Fatalf("exit = %d, want 0. output:\n%s", code, out)
	}
	if !strings.Contains(out, "PASS") {
		t.Errorf("output missing PASS:\n%s", out)
	}
}

func TestManifestGateFailsWhenRemoteDroppedACommand(t *testing.T) {
	remote := vendoredManifest(t)
	endpoints, ok := remote["endpoints"].([]any)
	if !ok || len(endpoints) == 0 {
		t.Fatalf("vendored manifest has no endpoints")
	}
	remote["endpoints"] = endpoints[1:]

	out, code := runManifestGate(t, remote)

	if code != 1 {
		t.Fatalf("exit = %d, want 1. output:\n%s", code, out)
	}
	if !strings.Contains(out, "FAIL") || !strings.Contains(out, "make update-manifest") {
		t.Errorf("output should fail pointing at make update-manifest:\n%s", out)
	}
}

func TestManifestGateIgnoresMetadataOnlyDifference(t *testing.T) {
	remote := vendoredManifest(t)
	remote["generated_at"] = "1999-01-01T00:00:00Z"
	remote["source"] = "linkanalabs/linkana@deadbeef"

	out, code := runManifestGate(t, remote)

	if code != 0 {
		t.Fatalf("exit = %d, want 0 (generated_at/source are not contract). output:\n%s", code, out)
	}
}

func TestManifestGateFailsOnUnreadableRemote(t *testing.T) {
	out, code := runManifestGate(t, map[string]any{"manifest_version": 0})

	if code != 1 {
		t.Fatalf("exit = %d, want 1. output:\n%s", code, out)
	}
	if !strings.Contains(out, "FAIL") {
		t.Errorf("output missing FAIL:\n%s", out)
	}
}
