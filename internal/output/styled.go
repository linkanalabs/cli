package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
)

// genericStyled renders any JSON-shaped value as human-friendly text: a
// column-aligned table for an array of objects, a key/value block for a
// single object, one line per element for an array of scalars. It reports
// ok=false when the value has no obvious tabular shape (mixed arrays,
// non-JSON data), letting the caller fall back to JSON.
func genericStyled(data any) (string, bool) {
	raw, err := json.Marshal(data)
	if err != nil {
		return "", false
	}
	node, err := decodeOrdered(raw)
	if err != nil {
		return "", false
	}

	switch v := node.(type) {
	case *orderedObject:
		return keyValueBlock(v), true
	case []any:
		return styledArray(v)
	default:
		return scalarText(v) + "\n", true
	}
}

func styledArray(items []any) (string, bool) {
	if len(items) == 0 {
		return "(no results)\n", true
	}

	objects := make([]*orderedObject, 0, len(items))
	scalars := true
	for _, item := range items {
		if o, ok := item.(*orderedObject); ok {
			objects = append(objects, o)
			scalars = false
			continue
		}
		if _, isArray := item.([]any); isArray {
			scalars = false
		}
	}

	switch {
	case len(objects) == len(items):
		return table(objects), true
	case scalars:
		var b strings.Builder
		for _, item := range items {
			b.WriteString(scalarText(item))
			b.WriteString("\n")
		}
		return b.String(), true
	default:
		return "", false
	}
}

// table renders objects as an aligned table. Columns follow the key order of
// the first object; keys seen only in later objects are appended in
// first-seen order.
func table(objects []*orderedObject) string {
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

	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, strings.Join(columns, "\t"))
	for _, o := range objects {
		cells := make([]string, len(columns))
		for i, k := range columns {
			if v, ok := o.vals[k]; ok {
				cells[i] = cellText(v)
			}
		}
		_, _ = fmt.Fprintln(w, strings.Join(cells, "\t"))
	}
	_ = w.Flush()
	return buf.String()
}

// keyValueBlock renders one object as aligned "key:  value" lines.
func keyValueBlock(o *orderedObject) string {
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 4, 2, ' ', 0)
	for _, k := range o.keys {
		_, _ = fmt.Fprintf(w, "%s:\t%s\n", k, cellText(o.vals[k]))
	}
	_ = w.Flush()
	return buf.String()
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
		return s
	case json.Number:
		return s.String()
	default:
		return fmt.Sprintf("%v", s)
	}
}

// orderedObject is a JSON object that remembers its key order, so styled
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
