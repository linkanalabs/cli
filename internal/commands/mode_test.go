package commands

import (
	"bytes"
	"strings"
	"testing"
)

func TestModeShowDefaultRead(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var out, errb bytes.Buffer
	code := runWith(strings.NewReader(""), []string{"mode", "--format", "json"}, &out, &errb)
	if code != 0 || !strings.Contains(out.String(), `"read"`) {
		t.Fatalf("code=%d out=%q", code, out.String())
	}
}

func TestModeWriteRequiresTTYAndConfirmation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	prev := isStdinTTY
	defer func() { isStdinTTY = prev }()

	// no TTY -> refuse
	isStdinTTY = func() bool { return false }
	var o, e bytes.Buffer
	if code := runWith(strings.NewReader("write\n"), []string{"mode", "write"}, &o, &e); code == 0 {
		t.Fatal("no-TTY should refuse")
	}
	// verify mode was NOT persisted after no-TTY refusal
	o.Reset()
	e.Reset()
	runWith(strings.NewReader(""), []string{"mode", "--format", "json"}, &o, &e)
	if !strings.Contains(o.String(), `"read"`) {
		t.Errorf("mode should still be read after no-TTY refusal, got %q", o.String())
	}

	// TTY + correct word -> enables write
	isStdinTTY = func() bool { return true }
	o.Reset()
	e.Reset()
	if code := runWith(strings.NewReader("write\n"), []string{"mode", "write"}, &o, &e); code != 0 {
		t.Fatalf("TTY+confirm should enable: code=%d err=%s", code, e.String())
	}
	// verify persisted via `mode` show
	o.Reset()
	e.Reset()
	runWith(strings.NewReader(""), []string{"mode", "--format", "json"}, &o, &e)
	if !strings.Contains(o.String(), `"write"`) {
		t.Errorf("expected write persisted, got %q", o.String())
	}

	// TTY + wrong word -> abort, stays write? reset to read first
	runWith(strings.NewReader(""), []string{"mode", "read"}, &bytes.Buffer{}, &bytes.Buffer{})
	isStdinTTY = func() bool { return true }
	o.Reset()
	e.Reset()
	if code := runWith(strings.NewReader("nope\n"), []string{"mode", "write"}, &o, &e); code == 0 {
		t.Fatal("wrong confirmation should abort")
	}
	// verify mode was NOT persisted after wrong-word abort
	o.Reset()
	e.Reset()
	runWith(strings.NewReader(""), []string{"mode", "--format", "json"}, &o, &e)
	if !strings.Contains(o.String(), `"read"`) {
		t.Errorf("mode should still be read after wrong-word abort, got %q", o.String())
	}
}

func TestModeReadIsFree(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var o, e bytes.Buffer
	if code := runWith(strings.NewReader(""), []string{"mode", "read"}, &o, &e); code != 0 {
		t.Fatalf("mode read should be free: %s", e.String())
	}
}

func TestModeShowStyledOutput(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var o, e bytes.Buffer
	code := runWith(strings.NewReader(""), []string{"mode", "--format", "styled"}, &o, &e)
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, e.String())
	}
	if !strings.Contains(o.String(), "read") {
		t.Errorf("styled output should contain mode, got %q", o.String())
	}
}

func TestReadLineEOF(t *testing.T) {
	// "hello" with no trailing newline — Read returns io.EOF; readLine must
	// return the accumulated bytes and nil error.
	got, err := readLine(strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("readLine: %v", err)
	}
	if got != "hello" {
		t.Errorf("readLine = %q, want %q", got, "hello")
	}
}
