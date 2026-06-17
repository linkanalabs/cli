// Package output renders command results as JSON (default) or styled text.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/mattn/go-isatty"
)

// isTerminal is a seam so tests can exercise the terminal-detection branch.
var isTerminal = isatty.IsTerminal

// Format selects how results are rendered.
const (
	FormatAuto   = "auto"
	FormatJSON   = "json"
	FormatStyled = "styled"
)

// Styler is implemented by results that can render themselves as styled text.
type Styler interface {
	Styled() string
}

// Render writes data to w in the requested format. FormatAuto resolves to
// styled on a terminal and JSON otherwise. Styled falls back to JSON when the
// data does not implement Styler.
func Render(w io.Writer, format string, data any) error {
	switch resolveFormat(format, w) {
	case FormatStyled:
		if s, ok := data.(Styler); ok {
			_, err := fmt.Fprint(w, s.Styled())
			return err
		}
		fallthrough
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
