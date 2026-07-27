// Package output renders command results as JSON (default) or styled text.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/mattn/go-isatty"
)

// isTerminal is a seam so tests can exercise the terminal-detection branch.
var isTerminal = isatty.IsTerminal

// Format selects how results are rendered.
const (
	FormatAuto     = "auto"
	FormatJSON     = "json"
	FormatStyled   = "styled"
	FormatMarkdown = "markdown"
	FormatIDs      = "ids"
	FormatCount    = "count"
)

// Formats lists every accepted --format value, in help order.
var Formats = []string{FormatAuto, FormatJSON, FormatStyled, FormatMarkdown, FormatIDs, FormatCount}

// Valid reports whether format is an accepted --format value.
func Valid(format string) bool {
	return slices.Contains(Formats, format)
}

// FormatList renders the accepted values for flag help and error messages.
func FormatList() string { return strings.Join(Formats, "|") }

// Styler is implemented by results that render themselves as styled text
// (diagnostic commands with bespoke layouts, e.g. doctor). Resource-shaped
// results should NOT implement it: they get the generic styled rendering.
type Styler interface {
	Styled() string
}

// Render writes data to w in the requested format. FormatAuto resolves to
// styled on a terminal and JSON otherwise. Styled uses the data's own Styler
// when implemented, then the generic renderer (table for an array of
// objects, key/value block for an object). Markdown always uses the generic
// renderer — a Styler paints the terminal, not a document. Both fall back to
// JSON when the data has no JSON-friendly shape. Ids and count read the JSON
// shape directly. An unrecognized format renders JSON; the CLI rejects one
// up front (see the root command) so no request is spent on a typo.
func Render(w io.Writer, format string, data any) error {
	switch resolveFormat(format, w) {
	case FormatStyled:
		if s, ok := data.(Styler); ok {
			_, err := fmt.Fprint(w, s.Styled())
			return err
		}
		if s, ok := genericStyled(data); ok {
			_, err := fmt.Fprint(w, s)
			return err
		}
		return renderJSON(w, data)
	case FormatMarkdown:
		if s, ok := genericMarkdown(data); ok {
			_, err := fmt.Fprint(w, s)
			return err
		}
		return renderJSON(w, data)
	case FormatIDs:
		return renderIDs(w, data)
	case FormatCount:
		return renderCount(w, data)
	default:
		return renderJSON(w, data)
	}
}

func renderJSON(w io.Writer, data any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

func resolveFormat(format string, w io.Writer) string {
	if format != FormatAuto {
		return format
	}
	if f, ok := w.(*os.File); ok && isTerminal(f.Fd()) {
		return FormatStyled
	}
	return FormatJSON
}
