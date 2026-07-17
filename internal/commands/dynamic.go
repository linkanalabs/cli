package commands

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/manifest"
)

// loadManifest is a seam so tests can substitute the embedded manifest.
var loadManifest = manifest.Load

// registerDynamic mounts the manifest-driven command tree under root. Manual
// commands are registered first and always win: any name collision at the same
// level silently skips the dynamic endpoint.
func registerDynamic(root *cobra.Command, m *manifest.Manifest) {
	for i := range m.Endpoints {
		registerEndpoint(root, &m.Endpoints[i])
	}
}

// registerEndpoint walks/creates the group chain for the endpoint's command
// path and attaches the dynamic leaf, unless a collision makes it back off.
func registerEndpoint(root *cobra.Command, e *manifest.Endpoint) {
	parent := root
	for _, name := range e.Command[:len(e.Command)-1] {
		child := findChild(parent, name)
		switch {
		case child == nil:
			group := &cobra.Command{
				Use:   name,
				Short: fmt.Sprintf("%s commands", name),
			}
			parent.AddCommand(group)
			parent = group
		case child.HasSubCommands():
			parent = child
		default:
			// The level name collides with a runnable leaf command; nesting
			// under it would change its behavior, so the manual command wins.
			return
		}
	}
	leaf := e.Command[len(e.Command)-1]
	if findChild(parent, leaf) != nil {
		return // existing command wins (manual takes precedence)
	}
	parent.AddCommand(newDynamicCmd(e))
}

// findChild returns the direct subcommand of parent named name, or nil.
func findChild(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

// newDynamicCmd builds the runnable leaf command for a manifest endpoint.
func newDynamicCmd(e *manifest.Endpoint) *cobra.Command {
	use := e.Command[len(e.Command)-1]
	for _, pp := range e.PathParams {
		use += " <" + pp + ">"
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: e.Summary,
		Long:  dynamicLong(e),
		Args:  cobra.ExactArgs(len(e.PathParams)),
		RunE:  runDynamic(e),
	}
	for i := range e.Params {
		addDynamicFlag(cmd, &e.Params[i])
	}
	return cmd
}

// addDynamicFlag registers one manifest param as a cobra flag. Native types
// map to native flags; date/datetime/decimal ride as strings; arrays of
// scalars repeat the flag; objects (and arrays of objects) take a JSON string.
func addDynamicFlag(cmd *cobra.Command, p *manifest.Param) {
	usage := p.Desc
	if len(p.Enum) > 0 {
		usage += " (one of: " + strings.Join(p.Enum, "|") + ")"
	}
	flags := cmd.Flags()
	switch p.Type {
	case manifest.TypeInteger:
		flags.Int64(p.Name, 0, usage)
	case manifest.TypeBoolean:
		flags.Bool(p.Name, false, usage)
	case manifest.TypeObject:
		flags.String(p.Name, "", usage+" (JSON object)")
	case manifest.TypeArray:
		if p.Item == manifest.TypeObject {
			flags.String(p.Name, "", usage+" (JSON array)")
		} else {
			flags.StringArray(p.Name, nil, usage+" (repeatable)")
		}
	default: // string, date, datetime, decimal
		flags.String(p.Name, "", usage)
	}
	if p.Required {
		_ = cmd.MarkFlagRequired(p.Name)
	}
}

// dynamicLong renders LLM-first long help: the endpoint description followed
// by a structured block with endpoint, auth, arguments, parameters and the
// response contract.
func dynamicLong(e *manifest.Endpoint) string {
	var b strings.Builder
	b.WriteString(e.Description)
	b.WriteString("\n\nEndpoint:\n  " + e.Method + " " + e.Path + "\n")
	b.WriteString("\nAuth: " + e.Auth + "\n")
	if len(e.PathParams) > 0 {
		b.WriteString("\nArguments (positional, in path order):\n")
		for i, pp := range e.PathParams {
			fmt.Fprintf(&b, "  %d. %s (path param :%s)\n", i+1, pp, pp)
		}
	}
	if len(e.Params) > 0 {
		b.WriteString("\nParameters:\n")
		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "  NAME\tTYPE\tIN\tREQUIRED\tDESCRIPTION")
		for _, p := range e.Params {
			typ := p.Type
			if p.Type == manifest.TypeArray && p.Item != "" {
				typ = "array of " + p.Item
			}
			required := "no"
			if p.Required {
				required = "yes"
			}
			desc := p.Desc
			if len(p.Enum) > 0 {
				desc += " (one of: " + strings.Join(p.Enum, "|") + ")"
			}
			_, _ = fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", p.Name, typ, p.In, required, desc)
		}
		_ = tw.Flush()
	}
	b.WriteString("\nResponse:\n  " + e.Response + "\n")
	return b.String()
}
