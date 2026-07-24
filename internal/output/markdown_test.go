package output

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGenericMarkdownArrayOfObjectsIsGFMTable(t *testing.T) {
	raw := json.RawMessage(`[
		{"template": "supplier_invite", "content": null, "updated_at": "2026-06-17"},
		{"template": "qualification_approved", "content": "hello", "updated_at": "2026-06-18"}
	]`)
	got, ok := genericMarkdown(raw)
	if !ok {
		t.Fatal("genericMarkdown not ok")
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected header + separator + 2 rows, got %d lines: %q", len(lines), got)
	}
	if lines[0] != "| template | content | updated_at |" {
		t.Errorf("header = %q", lines[0])
	}
	if lines[1] != "| --- | --- | --- |" {
		t.Errorf("separator = %q", lines[1])
	}
	if lines[2] != "| supplier_invite |  | 2026-06-17 |" {
		t.Errorf("row 1 = %q", lines[2])
	}
	if lines[3] != "| qualification_approved | hello | 2026-06-18 |" {
		t.Errorf("row 2 = %q", lines[3])
	}
}

func TestGenericMarkdownTableColumnsFollowFirstSeenOrder(t *testing.T) {
	// Column order comes from the JSON stream, and keys appearing only in
	// later objects are appended — same contract as the styled table.
	raw := json.RawMessage(`[{"z": 1, "a": 2}, {"z": 3, "a": 4, "extra": 5}]`)
	got, ok := genericMarkdown(raw)
	if !ok {
		t.Fatal("genericMarkdown not ok")
	}
	lines := strings.Split(got, "\n")
	if lines[0] != "| z | a | extra |" {
		t.Errorf("header = %q", lines[0])
	}
	// The first object has no "extra": the cell is empty, not missing.
	if lines[2] != "| 1 | 2 |  |" {
		t.Errorf("row 1 = %q", lines[2])
	}
}

func TestGenericMarkdownTableEscapesPipes(t *testing.T) {
	// An unescaped pipe in a value would forge an extra column.
	raw := json.RawMessage(`[{"name": "a|b", "tags": [{"display_name": "x|y"}]}]`)
	got, ok := genericMarkdown(raw)
	if !ok {
		t.Fatal("genericMarkdown not ok")
	}
	row := strings.Split(got, "\n")[2]
	if !strings.Contains(row, `a\|b`) {
		t.Errorf("pipe in scalar not escaped: %q", row)
	}
	if !strings.Contains(row, `x\|y`) {
		t.Errorf("pipe inside nested JSON not escaped: %q", row)
	}
	if strings.Count(row, "|")-strings.Count(row, `\|`) != 3 {
		t.Errorf("row has forged columns: %q", row)
	}
}

func TestGenericMarkdownMultilineValueStaysInOneRow(t *testing.T) {
	// An e-mail template body is enough to break a GFM table if the newline
	// reaches the cell.
	raw := json.RawMessage(`[{"template":"a","content":"line1\nline2\tcol"},{"template":"b","content":"ok"}]`)
	got, ok := genericMarkdown(raw)
	if !ok {
		t.Fatal("genericMarkdown not ok")
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected header + separator + 2 rows, got %d lines: %q", len(lines), got)
	}
	if !strings.Contains(lines[2], `line1\nline2\tcol`) {
		t.Errorf("row 1 should carry the escaped value: %q", lines[2])
	}
}

func TestGenericMarkdownObjectIsBoldLabels(t *testing.T) {
	raw := json.RawMessage(`{"id": "s_1", "name": "ACME", "tags": [{"display_name": "vip"}]}`)
	got, ok := genericMarkdown(raw)
	if !ok {
		t.Fatal("genericMarkdown not ok")
	}
	want := "**id:** s_1\n**name:** ACME\n" + `**tags:** [{"display_name":"vip"}]` + "\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGenericMarkdownEmptyArray(t *testing.T) {
	got, ok := genericMarkdown(json.RawMessage(`[]`))
	if !ok || got != "_(no results)_\n" {
		t.Errorf("got %q, ok=%v", got, ok)
	}
}

func TestGenericMarkdownScalarArrayIsBulletList(t *testing.T) {
	got, ok := genericMarkdown(json.RawMessage(`["a", "b", null]`))
	if !ok || got != "- a\n- b\n-\n" {
		t.Errorf("got %q, ok=%v", got, ok)
	}
}

func TestGenericMarkdownScalar(t *testing.T) {
	got, ok := genericMarkdown(json.RawMessage(`42`))
	if !ok || got != "42\n" {
		t.Errorf("got %q, ok=%v", got, ok)
	}
}

func TestGenericMarkdownNumbersKeepPrecision(t *testing.T) {
	got, ok := genericMarkdown(json.RawMessage(`[{"amount": 12345678901234567890.5}]`))
	if !ok {
		t.Fatal("genericMarkdown not ok")
	}
	if !strings.Contains(got, "12345678901234567890.5") {
		t.Errorf("number mangled: %q", got)
	}
}

func TestGenericMarkdownMixedArrayNotOK(t *testing.T) {
	if _, ok := genericMarkdown(json.RawMessage(`[{"a": 1}, "scalar"]`)); ok {
		t.Error("mixed array should not be ok")
	}
}

func TestGenericMarkdownArrayOfArraysNotOK(t *testing.T) {
	if _, ok := genericMarkdown(json.RawMessage(`[[1, 2], [3]]`)); ok {
		t.Error("array of arrays should not be ok")
	}
}

func TestGenericMarkdownInvalidJSONNotOK(t *testing.T) {
	if _, ok := genericMarkdown(json.RawMessage(`{broken`)); ok {
		t.Error("invalid JSON should not be ok")
	}
}

func TestGenericMarkdownUnmarshalableNotOK(t *testing.T) {
	if _, ok := genericMarkdown(make(chan int)); ok {
		t.Error("unmarshalable value should not be ok")
	}
}
