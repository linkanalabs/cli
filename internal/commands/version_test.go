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
	if got := (versionInfo{Version: "1.0.0"}).Styled(); got != "lk 1.0.0\n" {
		t.Errorf("Styled() = %q", got)
	}
}
