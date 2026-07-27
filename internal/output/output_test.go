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

func TestRenderStyledObjectIsKeyValueBlock(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, FormatStyled, plainData{Name: "z"}); err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if !strings.Contains(buf.String(), "name:") || !strings.Contains(buf.String(), "z") {
		t.Errorf("expected key/value block, got %q", buf.String())
	}
}

func TestRenderStyledFallsBackToJSONOnMixedArray(t *testing.T) {
	var buf bytes.Buffer
	// A mix of object and scalar has no tabular shape; falls back to JSON.
	if err := Render(&buf, FormatStyled, []any{plainData{Name: "z"}, "loose"}); err != nil {
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

func TestRenderMarkdownIgnoresStyler(t *testing.T) {
	// Styled() is ANSI/bespoke terminal output; markdown must come from the
	// generic renderer instead.
	var buf bytes.Buffer
	if err := Render(&buf, FormatMarkdown, styledData{Name: "y"}); err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if buf.String() != "- **name:** y\n" {
		t.Errorf("output = %q", buf.String())
	}
}

func TestRenderMarkdownFallsBackToJSONOnMixedArray(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, FormatMarkdown, []any{plainData{Name: "z"}, "loose"}); err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if !strings.Contains(buf.String(), `"name": "z"`) {
		t.Errorf("expected JSON fallback, got %q", buf.String())
	}
}

func TestRenderIDsFormat(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, FormatIDs, []any{map[string]any{"id": "s_1"}}); err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if buf.String() != "s_1\n" {
		t.Errorf("output = %q", buf.String())
	}
}

func TestRenderCountFormat(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, FormatCount, []any{1, 2, 3}); err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if buf.String() != "3\n" {
		t.Errorf("output = %q", buf.String())
	}
}

func TestValid(t *testing.T) {
	for _, f := range Formats {
		if !Valid(f) {
			t.Errorf("Valid(%q) = false", f)
		}
	}
	if Valid("markdwon") {
		t.Error("Valid should reject an unknown format")
	}
}

func TestFormatList(t *testing.T) {
	if got := FormatList(); got != "auto|json|styled|markdown|ids|count" {
		t.Errorf("FormatList() = %q", got)
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
