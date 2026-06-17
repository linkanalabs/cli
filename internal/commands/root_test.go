package commands

import (
	"bytes"
	"strings"
	"testing"
)

func TestSetVersion(t *testing.T) {
	orig := version
	defer func() { version = orig }()

	SetVersion("1.2.3")
	if version != "1.2.3" {
		t.Errorf("version = %q", version)
	}
	SetVersion("")
	if version != "1.2.3" {
		t.Errorf("empty SetVersion should not change version, got %q", version)
	}
}

func TestRunHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"--help"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "lk") {
		t.Errorf("help output = %q", out.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"nope"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(errOut.String(), "error:") {
		t.Errorf("stderr = %q", errOut.String())
	}
}
