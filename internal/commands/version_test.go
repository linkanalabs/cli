package commands

import (
	"bytes"
	"strings"
	"testing"
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
	var out, errOut bytes.Buffer
	code := run([]string{"version", "--format", "json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}
	for _, want := range []string{`"generated_at": "2026-07-17T00:00:00Z"`, `"source": "linkanalabs/linkana@development"`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q: %q", want, out.String())
		}
	}
}

func TestVersionShowsManifestStyled(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"version", "--format", "styled"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "manifest: 2026-07-17T00:00:00Z (linkanalabs/linkana@development)") {
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
