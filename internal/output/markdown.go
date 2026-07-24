package output

import (
	"fmt"
	"strings"
)

// genericMarkdown renders any JSON-shaped value as GitHub-flavored Markdown:
// a table for an array of objects, bold-label lines for a single object, a
// bullet list for an array of scalars. It reports ok=false when the value has
// no obvious document shape (mixed arrays, non-JSON data), letting the caller
// fall back to JSON.
func genericMarkdown(data any) (string, bool) {
	node, ok := jsonShape(data)
	if !ok {
		return "", false
	}

	switch v := node.(type) {
	case *orderedObject:
		return markdownDetail(v), true
	case []any:
		return markdownArray(v)
	default:
		return scalarText(v) + "\n", true
	}
}

func markdownArray(items []any) (string, bool) {
	if len(items) == 0 {
		return "_(no results)_\n", true
	}

	objects, scalars := arrayShape(items)

	switch {
	case len(objects) == len(items):
		return markdownTable(objects), true
	case scalars:
		var b strings.Builder
		for _, item := range items {
			b.WriteString(strings.TrimRight("- "+scalarText(item), " "))
			b.WriteString("\n")
		}
		return b.String(), true
	default:
		return "", false
	}
}

// markdownTable renders objects as a GFM table. Columns follow the key order
// of the first object; keys seen only in later objects are appended in
// first-seen order.
func markdownTable(objects []*orderedObject) string {
	columns := columnsOf(objects)

	var b strings.Builder
	headers := make([]string, len(columns))
	separators := make([]string, len(columns))
	for i, c := range columns {
		headers[i] = escapePipes(c)
		separators[i] = "---"
	}
	markdownRow(&b, headers)
	markdownRow(&b, separators)

	for _, o := range objects {
		cells := make([]string, len(columns))
		for i, k := range columns {
			if v, ok := o.vals[k]; ok {
				cells[i] = escapePipes(cellText(v))
			}
		}
		markdownRow(&b, cells)
	}
	return b.String()
}

// markdownDetail renders one object as "**key:** value" lines.
func markdownDetail(o *orderedObject) string {
	var b strings.Builder
	for _, k := range o.keys {
		_, _ = fmt.Fprintf(&b, "**%s:** %s\n", k, cellText(o.vals[k]))
	}
	return b.String()
}

func markdownRow(b *strings.Builder, cells []string) {
	b.WriteString("| ")
	b.WriteString(strings.Join(cells, " | "))
	b.WriteString(" |\n")
}

// escapePipes keeps a value inside its own table cell. escapeLayout already
// neutralized the newlines and control characters; a raw pipe is what is left
// to forge an extra column.
func escapePipes(s string) string { return strings.ReplaceAll(s, "|", `\|`) }
