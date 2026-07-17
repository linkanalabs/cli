package commands

import (
	"strings"

	"github.com/spf13/cobra"
)

// surfaceLines walks the full command tree (manual + dynamic) and returns one
// line per command path. It backs the SURFACE.txt golden, which makes any
// drift or takeover visible when the manifest changes.
func surfaceLines(root *cobra.Command) []string {
	var lines []string
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		lines = append(lines, c.CommandPath())
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
	return lines
}

// surface renders the command surface as the SURFACE.txt file contents.
func surface(root *cobra.Command) string {
	return strings.Join(surfaceLines(root), "\n") + "\n"
}
