package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestRenderIDsArrayOfObjects(t *testing.T) {
	var buf bytes.Buffer
	raw := json.RawMessage(`[{"id":"s_1","name":"Acme"},{"id":"s_2","name":"Globex"}]`)
	if err := renderIDs(&buf, raw); err != nil {
		t.Fatalf("renderIDs error: %v", err)
	}
	if buf.String() != "s_1\ns_2\n" {
		t.Errorf("got %q", buf.String())
	}
}

func TestRenderIDsSkipsRecordsWithoutUsableID(t *testing.T) {
	// A missing, null, empty or non-scalar id is not addressable: emitting a
	// blank line would feed an empty argument into the next command.
	var buf bytes.Buffer
	raw := json.RawMessage(`[{"id":"s_1"},{"name":"none"},{"id":null},{"id":""},{"id":{"x":1}},{"id":"s_6"}]`)
	if err := renderIDs(&buf, raw); err != nil {
		t.Fatalf("renderIDs error: %v", err)
	}
	if buf.String() != "s_1\ns_6\n" {
		t.Errorf("got %q", buf.String())
	}
}

func TestRenderIDsFailsWhenNoRecordHasAnID(t *testing.T) {
	// The SRM e-mail templates are keyed by "template". Silence here would
	// read to an agent exactly like an empty list.
	var buf bytes.Buffer
	raw := json.RawMessage(`[{"template":"supplier_invite"},{"template":"qualification_approved"}]`)
	err := renderIDs(&buf, raw)
	if err == nil {
		t.Fatal("expected an error instead of silent empty output")
	}
	for _, want := range []string{"2 record", `"id"`, "--format json"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
	if buf.String() != "" {
		t.Errorf("stdout should stay empty, got %q", buf.String())
	}
}

func TestRenderIDsEmptyResponseIsNotAnError(t *testing.T) {
	// No records is a legitimate answer; no id field is not.
	for _, raw := range []string{`[]`, `null`} {
		var buf bytes.Buffer
		if err := renderIDs(&buf, json.RawMessage(raw)); err != nil {
			t.Fatalf("renderIDs(%s) error: %v", raw, err)
		}
		if buf.String() != "" {
			t.Errorf("renderIDs(%s) = %q, want empty", raw, buf.String())
		}
	}
}

func TestRenderIDsSingleObject(t *testing.T) {
	var buf bytes.Buffer
	if err := renderIDs(&buf, json.RawMessage(`{"id":42,"name":"Acme"}`)); err != nil {
		t.Fatalf("renderIDs error: %v", err)
	}
	if buf.String() != "42\n" {
		t.Errorf("got %q", buf.String())
	}
}

func TestRenderIDsSingleObjectWithoutIDFails(t *testing.T) {
	var buf bytes.Buffer
	if err := renderIDs(&buf, json.RawMessage(`{"template":"supplier_invite"}`)); err == nil {
		t.Error("expected an error for a record with no id")
	}
}

func TestRenderIDsKeepsLargeIntegerPrecision(t *testing.T) {
	var buf bytes.Buffer
	if err := renderIDs(&buf, json.RawMessage(`[{"id":12345678901234567890}]`)); err != nil {
		t.Fatalf("renderIDs error: %v", err)
	}
	if buf.String() != "12345678901234567890\n" {
		t.Errorf("id mangled: %q", buf.String())
	}
}

func TestRenderIDsScalarArrayFailsInsteadOfPrintingNothing(t *testing.T) {
	var buf bytes.Buffer
	if err := renderIDs(&buf, json.RawMessage(`["a","b"]`)); err == nil {
		t.Error("expected an error: the response has records but no ids")
	}
}

func TestRenderIDsFallsBackToJSONOnUndecodableData(t *testing.T) {
	var buf bytes.Buffer
	if err := renderIDs(&buf, make(chan int)); err == nil {
		t.Error("expected the JSON renderer's error to surface")
	}
}

func TestRenderIDsFallsBackToJSONWhenTheTreeCannotBeDecoded(t *testing.T) {
	// The fallback must actually render JSON, not just avoid crashing.
	orig := decodeShape
	defer func() { decodeShape = orig }()
	decodeShape = func([]byte) (any, error) { return nil, errors.New("boom") }

	var buf bytes.Buffer
	if err := renderIDs(&buf, map[string]any{"id": "s_1"}); err != nil {
		t.Fatalf("renderIDs error: %v", err)
	}
	if !strings.Contains(buf.String(), `"id": "s_1"`) {
		t.Errorf("expected JSON fallback, got %q", buf.String())
	}
}

// failingWriter fails on the second write, so a multi-record render has to
// propagate the error instead of swallowing it.
type failingWriter struct{ writes int }

func (f *failingWriter) Write(p []byte) (int, error) {
	f.writes++
	if f.writes > 1 {
		return 0, errors.New("disk full")
	}
	return len(p), nil
}

func TestRenderIDsPropagatesWriteError(t *testing.T) {
	w := &failingWriter{}
	err := renderIDs(w, json.RawMessage(`[{"id":"s_1"},{"id":"s_2"}]`))
	if err == nil {
		t.Fatal("expected the write error to surface")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("error = %v", err)
	}
}

func TestRenderCountShapes(t *testing.T) {
	cases := map[string]string{
		`[{"id":1},{"id":2},{"id":3}]`: "3\n",
		`["a","b"]`:                    "2\n",
		`[]`:                           "0\n",
		`{"id":1}`:                     "1\n",
		`null`:                         "0\n",
		`42`:                           "1\n",
	}
	for raw, want := range cases {
		var buf bytes.Buffer
		if err := renderCount(&buf, json.RawMessage(raw)); err != nil {
			t.Fatalf("renderCount(%s) error: %v", raw, err)
		}
		if buf.String() != want {
			t.Errorf("renderCount(%s) = %q, want %q", raw, buf.String(), want)
		}
	}
}

func TestRenderCountFallsBackToJSONOnUndecodableData(t *testing.T) {
	var buf bytes.Buffer
	if err := renderCount(&buf, make(chan int)); err == nil {
		t.Error("expected the JSON renderer's error to surface")
	}
}

func TestRenderCountPropagatesWriteError(t *testing.T) {
	w := &failingWriter{writes: 1}
	if err := renderCount(w, json.RawMessage(`[]`)); err == nil {
		t.Error("expected the write error to surface")
	}
}

func TestRenderCountOfStructCountsAsOne(t *testing.T) {
	var buf bytes.Buffer
	if err := renderCount(&buf, plainData{Name: "x"}); err != nil {
		t.Fatalf("renderCount error: %v", err)
	}
	if buf.String() != "1\n" {
		t.Errorf("got %q", buf.String())
	}
}
