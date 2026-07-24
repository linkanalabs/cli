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

func TestRenderIDsSkipsObjectsWithoutID(t *testing.T) {
	var buf bytes.Buffer
	raw := json.RawMessage(`[{"id":"s_1"},{"name":"no id here"},{"id":"s_3"}]`)
	if err := renderIDs(&buf, raw); err != nil {
		t.Fatalf("renderIDs error: %v", err)
	}
	if buf.String() != "s_1\ns_3\n" {
		t.Errorf("got %q", buf.String())
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

func TestRenderIDsKeepsLargeIntegerPrecision(t *testing.T) {
	var buf bytes.Buffer
	if err := renderIDs(&buf, json.RawMessage(`[{"id":12345678901234567890}]`)); err != nil {
		t.Fatalf("renderIDs error: %v", err)
	}
	if buf.String() != "12345678901234567890\n" {
		t.Errorf("id mangled: %q", buf.String())
	}
}

func TestRenderIDsScalarShapesEmitNothing(t *testing.T) {
	for _, raw := range []string{`["a","b"]`, `42`, `null`, `[]`} {
		var buf bytes.Buffer
		if err := renderIDs(&buf, json.RawMessage(raw)); err != nil {
			t.Fatalf("renderIDs(%s) error: %v", raw, err)
		}
		if buf.String() != "" {
			t.Errorf("renderIDs(%s) = %q, want empty", raw, buf.String())
		}
	}
}

func TestRenderIDsFallsBackToJSONOnUndecodableData(t *testing.T) {
	// A Styler-only value still marshals; anything that does not decode as
	// JSON falls back to the JSON renderer, which surfaces the real error.
	var buf bytes.Buffer
	if err := renderIDs(&buf, make(chan int)); err == nil {
		t.Error("expected error for unmarshalable value")
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
		t.Error("expected error for unmarshalable value")
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
