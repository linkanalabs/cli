package commands

import (
	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/output"
)

// versionInfo is the payload for the version command.
type versionInfo struct {
	Version string `json:"version"`
}

// Styled renders the version as plain text.
func (v versionInfo) Styled() string {
	return "lk " + v.Version + "\n"
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return output.Render(cmd.OutOrStdout(), formatFlag(cmd), versionInfo{Version: version})
		},
	}
}
