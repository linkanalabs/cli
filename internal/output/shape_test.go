package output

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestJSONShapeReportsDecodeFailure(t *testing.T) {
	orig := decodeShape
	defer func() { decodeShape = orig }()
	decodeShape = func([]byte) (any, error) { return nil, errors.New("boom") }

	if _, ok := jsonShape(json.RawMessage(`{"a":1}`)); ok {
		t.Error("jsonShape should report not-ok when the tree cannot be decoded")
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
