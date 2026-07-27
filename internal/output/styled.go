package output

import (
	"bytes"
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
	node, ok := jsonShape(data)
	if !ok {
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

	objects, scalars := arrayShape(items)

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
	columns := columnsOf(objects)

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
