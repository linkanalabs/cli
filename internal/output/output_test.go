package output

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

type styledData struct {
	Name string `json:"name"`
}

func (s styledData) Styled() string { return "STYLED:" + s.Name }

type plainData struct {
	Name string `json:"name"`
}

func TestRenderJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, FormatJSON, plainData{Name: "x"}); err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if !strings.Contains(buf.String(), `"name": "x"`) {
		t.Errorf("output = %q", buf.String())
	}
}

func TestRenderStyledUsesStyler(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, FormatStyled, styledData{Name: "y"}); err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if buf.String() != "STYLED:y" {
		t.Errorf("output = %q", buf.String())
	}
}

func TestRenderStyledFallsBackToJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, FormatStyled, plainData{Name: "z"}); err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if !strings.Contains(buf.String(), `"name": "z"`) {
		t.Errorf("expected JSON fallback, got %q", buf.String())
	}
}

func TestRenderAutoNonTerminalIsJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, FormatAuto, styledData{Name: "a"}); err != nil {
		t.Fatalf("Render error: %v", err)
	}
	// A bytes.Buffer is not a terminal, so auto resolves to JSON.
	if !strings.Contains(buf.String(), `"name": "a"`) {
		t.Errorf("expected JSON for non-terminal, got %q", buf.String())
	}
}

func TestResolveFormatExplicit(t *testing.T) {
	var buf bytes.Buffer
	if got := resolveFormat(FormatJSON, &buf); got != FormatJSON {
		t.Errorf("resolveFormat = %q", got)
	}
}

func TestResolveFormatAutoTerminal(t *testing.T) {
	orig := isTerminal
	defer func() { isTerminal = orig }()
	isTerminal = func(uintptr) bool { return true }

	// os.Stdout is an *os.File, so the terminal branch is reached.
	if got := resolveFormat(FormatAuto, os.Stdout); got != FormatStyled {
		t.Errorf("resolveFormat(auto, terminal) = %q, want styled", got)
	}
}
