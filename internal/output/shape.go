package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// decodeShape is a seam: decodeOrdered cannot fail on json.Marshal output, so
// tests override it to exercise jsonShape's defensive branch.
var decodeShape = decodeOrdered

// jsonShape marshals data and decodes it back into an ordered JSON tree, the
// common input of every non-JSON renderer (styled, markdown, ids, count). It
// reports ok=false when the value has no JSON-friendly form.
func jsonShape(data any) (any, bool) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, false
	}
	node, err := decodeShape(raw)
	if err != nil {
		return nil, false
	}
	return node, true
}

// arrayShape classifies a decoded JSON array: it returns the elements that are
// objects (in order) and reports whether every element is a scalar. A caller
// has a tabular array when len(objects) == len(items), a list when allScalars,
// and no renderable shape otherwise.
func arrayShape(items []any) (objects []*orderedObject, allScalars bool) {
	objects = make([]*orderedObject, 0, len(items))
	allScalars = true
	for _, item := range items {
		if o, ok := item.(*orderedObject); ok {
			objects = append(objects, o)
			allScalars = false
			continue
		}
		if _, isArray := item.([]any); isArray {
			allScalars = false
		}
	}
	return objects, allScalars
}

// columnsOf returns the column names for a set of objects: the key order of
// the first object, with keys seen only in later objects appended in
// first-seen order.
func columnsOf(objects []*orderedObject) []string {
	var columns []string
	seen := map[string]bool{}
	for _, o := range objects {
		for _, k := range o.keys {
			if !seen[k] {
				seen[k] = true
				columns = append(columns, k)
			}
		}
	}
	return columns
}

// cellText renders a value inside a table cell or key/value line. Nested
// objects and arrays collapse to compact JSON.
func cellText(v any) string {
	switch v.(type) {
	case *orderedObject, []any:
		compact, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(compact)
	default:
		return scalarText(v)
	}
}

func scalarText(v any) string {
	switch s := v.(type) {
	case nil:
		return ""
	case string:
		return escapeLayout(s)
	case json.Number:
		return s.String()
	default:
		return fmt.Sprintf("%v", s)
	}
}

// escapeLayout makes a backend string safe to place in one table cell or
// key/value line. Newlines and tabs would otherwise forge extra rows and
// columns (an e-mail template's multi-line content is enough to do it), and
// escape sequences would let response data drive the terminal.
func escapeLayout(s string) string {
	if strings.IndexFunc(s, needsEscape) < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case needsEscape(r):
			_, _ = fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func needsEscape(r rune) bool { return unicode.IsControl(r) }

// orderedObject is a JSON object that remembers its key order, so rendered
// output shows columns and fields in the order the backend emitted them.
type orderedObject struct {
	keys []string
	vals map[string]any
}

// MarshalJSON preserves the original key order, so nested objects collapse
// to compact JSON without reshuffling.
func (o *orderedObject) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		val, err := json.Marshal(o.vals[k])
		if err != nil {
			return nil, err
		}
		buf.Write(val)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// decodeOrdered parses JSON preserving object key order (encoding/json maps
// would lose it) and numbers as json.Number (no float mangling).
func decodeOrdered(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	node, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("unexpected trailing JSON data")
	}
	return node, nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil // string, json.Number, bool or nil
	}

	switch delim {
	case '{':
		o := &orderedObject{vals: map[string]any{}}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyTok.(string)
			if !ok {
				return nil, fmt.Errorf("object key is not a string: %v", keyTok)
			}
			val, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			o.keys = append(o.keys, key)
			o.vals[key] = val
		}
		if _, err := dec.Token(); err != nil { // consume '}'
			return nil, err
		}
		return o, nil
	case '[':
		arr := []any{}
		for dec.More() {
			val, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			arr = append(arr, val)
		}
		if _, err := dec.Token(); err != nil { // consume ']'
			return nil, err
		}
		return arr, nil
	default:
		return nil, fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
}
