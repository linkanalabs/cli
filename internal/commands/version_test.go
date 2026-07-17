package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/linkanalabs/cli/internal/manifest"
)

func TestVersionCommandJSON(t *testing.T) {
	orig := version
	defer func() { version = orig }()
	version = "9.9.9"

	var out, errOut bytes.Buffer
	code := run([]string{"version", "--format", "json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"version": "9.9.9"`) {
		t.Errorf("output = %q", out.String())
	}
}

func TestVersionInfoStyled(t *testing.T) {
	if got := (versionInfo{Version: "1.0.0"}).Styled(); got != "lk 1.0.0\nmanifest: (unavailable)\n" {
		t.Errorf("Styled() = %q", got)
	}
}

func TestVersionShowsManifestJSON(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatalf("manifest.Load: %v", err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"version", "--format", "json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}
	// Compare against the embedded manifest itself: generated_at/source are
	// volatile and change on every `make update-manifest`.
	wants := []string{
		`"generated_at": "` + m.GeneratedAt + `"`,
		`"source": "` + m.Source + `"`,
	}
	for _, want := range wants {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q: %q", want, out.String())
		}
	}
}

func TestVersionShowsManifestStyled(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatalf("manifest.Load: %v", err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"version", "--format", "styled"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "manifest: "+m.GeneratedAt+" ("+m.Source+")") {
		t.Errorf("output = %q", out.String())
	}
}

func TestVersionManifestLoadError(t *testing.T) {
	swapManifest(t, nil, errSilent)
	var out, errOut bytes.Buffer
	code := run([]string{"version", "--format", "json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"manifest": null`) {
		t.Errorf("output = %q", out.String())
	}
}
