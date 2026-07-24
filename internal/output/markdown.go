package output

import (
	"fmt"
	"strings"
)

// genericMarkdown renders any JSON-shaped value as GitHub-flavored Markdown:
// a table for an array of objects, a bullet list of bold labels for a single
// object, a bullet list for an array of scalars. It reports ok=false when the
// value has no obvious document shape (mixed arrays, objects without keys,
// non-JSON data), letting the caller fall back to JSON.
func genericMarkdown(data any) (string, bool) {
	node, ok := jsonShape(data)
	if !ok {
		return "", false
	}

	switch v := node.(type) {
	case nil:
		return noResults, true
	case *orderedObject:
		if len(v.keys) == 0 {
			// A keyless object would render as a blank page: say it out loud,
			// like null and [] do.
			return noResults, true
		}
		return markdownDetail(v), true
	case []any:
		return markdownArray(v)
	default:
		return scalarText(v) + "\n", true
	}
}

const noResults = "_(no results)_\n"

func markdownArray(items []any) (string, bool) {
	if len(items) == 0 {
		return noResults, true
	}

	objects, scalars := arrayShape(items)

	switch {
	case len(objects) == len(items):
		columns := columnsOf(objects)
		if len(columns) == 0 {
			// Keyless objects would produce a separator row without any
			// dashes, which no Markdown parser reads as a table.
			return "", false
		}
		return markdownTable(objects, columns), true
	case scalars:
		var b strings.Builder
		for _, item := range items {
			// A value's trailing spaces would become a Markdown hard break
			// and split the list item in two.
			b.WriteString(strings.TrimRight("- "+markdownCell(item), " "))
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
func markdownTable(objects []*orderedObject, columns []string) string {
	var b strings.Builder
	headers := make([]string, len(columns))
	separators := make([]string, len(columns))
	for i, c := range columns {
		headers[i] = markdownKey(c)
		separators[i] = "---"
	}
	markdownRow(&b, headers)
	markdownRow(&b, separators)

	for _, o := range objects {
		cells := make([]string, len(columns))
		for i, k := range columns {
			if v, ok := o.vals[k]; ok {
				cells[i] = markdownCell(v)
			}
		}
		markdownRow(&b, cells)
	}
	return b.String()
}

// markdownDetail renders one object as a bullet list of "**key:** value".
// Plain consecutive lines would collapse into a single paragraph in any
// CommonMark renderer; list items survive as separate lines everywhere.
func markdownDetail(o *orderedObject) string {
	var b strings.Builder
	for _, k := range o.keys {
		_, _ = fmt.Fprintf(&b, "- **%s:** %s\n", markdownKey(k), markdownCell(o.vals[k]))
	}
	return b.String()
}

func markdownRow(b *strings.Builder, cells []string) {
	b.WriteString("| ")
	b.WriteString(strings.Join(cells, " | "))
	b.WriteString(" |\n")
}

// markdownCell renders one value. Nested objects and arrays go inside a code
// span so their compact JSON survives verbatim — escaping it instead would
// hand the reader JSON whose own backslash escapes a renderer already ate.
func markdownCell(v any) string {
	switch v.(type) {
	case *orderedObject, []any:
		return codeSpan(cellText(v))
	default:
		return escapeMarkdown(scalarText(v))
	}
}

// markdownKey renders an object key as a header or a label. A key is data too:
// a newline inside one would break the table apart.
func markdownKey(k string) string { return escapeMarkdown(escapeLayout(k)) }

// escapeMarkdown keeps a scalar readable and inert. The table splits its row
// on an unescaped pipe, a renderer would swallow a lone backslash, and
// brackets would turn supplier-controlled text into a clickable link.
var markdownEscaper = strings.NewReplacer(`\`, `\\`, "|", `\|`, "[", `\[`, "]", `\]`)

func escapeMarkdown(s string) string { return markdownEscaper.Replace(s) }

// codeSpan wraps text in a backtick fence long enough to survive any run of
// backticks inside it. The pipe still needs escaping: a table row is split
// before inline code is parsed, and GFM puts the pipe back inside the span.
func codeSpan(s string) string {
	fence := "`"
	for strings.Contains(s, fence) {
		fence += "`"
	}
	pad := ""
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		pad = " "
	}
	return fence + pad + escapePipes(s) + pad + fence
}

func escapePipes(s string) string { return strings.ReplaceAll(s, "|", `\|`) }
