package output

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGenericStyledArrayOfObjectsIsTable(t *testing.T) {
	raw := json.RawMessage(`[
		{"template": "supplier_invite", "content": null, "updated_at": "2026-06-17"},
		{"template": "qualification_approved", "content": "hello", "updated_at": "2026-06-18"}
	]`)
	got, ok := genericStyled(raw)
	if !ok {
		t.Fatal("genericStyled not ok")
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected header + 2 rows, got %d lines: %q", len(lines), got)
	}
	if !strings.HasPrefix(lines[0], "template") || !strings.Contains(lines[0], "updated_at") {
		t.Errorf("header = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "supplier_invite") {
		t.Errorf("row 1 = %q", lines[1])
	}
	if !strings.Contains(lines[2], "hello") {
		t.Errorf("row 2 = %q", lines[2])
	}
}

func TestGenericStyledTableColumnsFollowFirstSeenOrder(t *testing.T) {
	// Key order must come from the JSON stream, not Go map iteration, and
	// keys appearing only in later objects are appended.
	raw := json.RawMessage(`[{"z": 1, "a": 2}, {"z": 3, "a": 4, "extra": 5}]`)
	got, ok := genericStyled(raw)
	if !ok {
		t.Fatal("genericStyled not ok")
	}
	header := strings.Split(got, "\n")[0]
	z, a, extra := strings.Index(header, "z"), strings.Index(header, "a"), strings.Index(header, "extra")
	if z >= a || a >= extra {
		t.Errorf("column order wrong: %q", header)
	}
}

func TestGenericStyledObjectIsKeyValueBlock(t *testing.T) {
	raw := json.RawMessage(`{"id": "s_1", "name": "ACME", "tags": [{"display_name": "vip"}]}`)
	got, ok := genericStyled(raw)
	if !ok {
		t.Fatal("genericStyled not ok")
	}
	if !strings.Contains(got, "id:") || !strings.Contains(got, "ACME") {
		t.Errorf("block = %q", got)
	}
	// Nested values collapse to compact JSON with preserved key order.
	if !strings.Contains(got, `[{"display_name":"vip"}]`) {
		t.Errorf("nested value = %q", got)
	}
}

func TestGenericStyledEmptyArray(t *testing.T) {
	got, ok := genericStyled(json.RawMessage(`[]`))
	if !ok || got != "(no results)\n" {
		t.Errorf("got %q, ok=%v", got, ok)
	}
}

func TestGenericStyledScalarArray(t *testing.T) {
	got, ok := genericStyled(json.RawMessage(`["a", "b", null]`))
	if !ok || got != "a\nb\n\n" {
		t.Errorf("got %q, ok=%v", got, ok)
	}
}

func TestGenericStyledScalar(t *testing.T) {
	got, ok := genericStyled(json.RawMessage(`42`))
	if !ok || got != "42\n" {
		t.Errorf("got %q, ok=%v", got, ok)
	}
}

func TestGenericStyledNumbersKeepPrecision(t *testing.T) {
	got, ok := genericStyled(json.RawMessage(`[{"amount": 12345678901234567890.5}]`))
	if !ok {
		t.Fatal("genericStyled not ok")
	}
	if !strings.Contains(got, "12345678901234567890.5") {
		t.Errorf("number mangled: %q", got)
	}
}

func TestGenericStyledMixedArrayNotOK(t *testing.T) {
	if _, ok := genericStyled(json.RawMessage(`[{"a": 1}, "scalar"]`)); ok {
		t.Error("mixed array should not be ok")
	}
}

func TestGenericStyledInvalidJSONNotOK(t *testing.T) {
	if _, ok := genericStyled(json.RawMessage(`{broken`)); ok {
		t.Error("invalid JSON should not be ok")
	}
}

func TestGenericStyledUnmarshalableNotOK(t *testing.T) {
	if _, ok := genericStyled(make(chan int)); ok {
		t.Error("unmarshalable value should not be ok")
	}
}

func TestDecodeOrderedRejectsTrailingData(t *testing.T) {
	if _, err := decodeOrdered([]byte(`1 2`)); err == nil {
		t.Error("expected error for trailing data")
	}
}

func TestDecodeOrderedRejectsTruncatedInput(t *testing.T) {
	for _, raw := range []string{`{`, `{"a":`, `[1,`, `{"a"`, `[`} {
		if _, err := decodeOrdered([]byte(raw)); err == nil {
			t.Errorf("expected error for %q", raw)
		}
	}
}

func TestScalarTextBool(t *testing.T) {
	if got := scalarText(true); got != "true" {
		t.Errorf("scalarText(true) = %q", got)
	}
}

func TestCellTextFallsBackOnUnmarshalableNested(t *testing.T) {
	// A nested object carrying an unmarshalable value exercises the error
	// fallback of cellText (unreachable through decodeOrdered output).
	bad := &orderedObject{keys: []string{"ch"}, vals: map[string]any{"ch": make(chan int)}}
	if got := cellText(bad); got == "" {
		t.Error("expected non-empty fallback text")
	}
}

func TestOrderedObjectMarshalJSONErrorOnBadValue(t *testing.T) {
	bad := &orderedObject{keys: []string{"ch"}, vals: map[string]any{"ch": make(chan int)}}
	if _, err := bad.MarshalJSON(); err == nil {
		t.Error("expected marshal error")
	}
}

func TestGenericStyledArrayOfArraysNotOK(t *testing.T) {
	// Arrays nested directly in the top-level array have no tabular shape.
	if _, ok := genericStyled(json.RawMessage(`[[1, 2], [3]]`)); ok {
		t.Error("array of arrays should not be ok")
	}
}

func TestGenericStyledBooleanScalar(t *testing.T) {
	got, ok := genericStyled(json.RawMessage(`true`))
	if !ok || got != "true\n" {
		t.Errorf("got %q, ok=%v", got, ok)
	}
}

func TestScalarTextEscapesLayoutBreakers(t *testing.T) {
	// A multi-line template body must stay in one cell instead of forging
	// extra table rows.
	raw := json.RawMessage(`[{"template":"a","content":"line1\nline2\tcol"},{"template":"b","content":"ok"}]`)
	got, ok := genericStyled(raw)
	if !ok {
		t.Fatal("genericStyled not ok")
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected header + 2 rows, got %d lines: %q", len(lines), got)
	}
	if !strings.Contains(lines[1], `line1\nline2\tcol`) {
		t.Errorf("row 1 should carry the escaped value: %q", lines[1])
	}
}

func TestScalarTextEscapesControlSequences(t *testing.T) {
	raw := json.RawMessage(`{"name": "\u001b[31mred\u001b[0m"}`)
	got, ok := genericStyled(raw)
	if !ok {
		t.Fatal("genericStyled not ok")
	}
	if strings.ContainsRune(got, 0x1b) {
		t.Errorf("escape sequence reached the terminal: %q", got)
	}
	if !strings.Contains(got, `\x1b[31mred`) {
		t.Errorf("expected escaped sequence, got %q", got)
	}
}

func TestScalarTextLeavesPlainStringsUntouched(t *testing.T) {
	if got := scalarText("Acme Ltda — São Paulo"); got != "Acme Ltda — São Paulo" {
		t.Errorf("plain string altered: %q", got)
	}
}

func TestScalarTextEscapesCarriageReturn(t *testing.T) {
	if got := scalarText("a\rb"); got != `a\rb` {
		t.Errorf("scalarText = %q", got)
	}
}
