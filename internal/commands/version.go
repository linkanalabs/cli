package commands

import (
	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/output"
)

// manifestInfo is the embedded-manifest metadata shown by the version command.
type manifestInfo struct {
	GeneratedAt string `json:"generated_at"`
	Source      string `json:"source"`
}

// versionInfo is the payload for the version command. Manifest is nil when
// the embedded manifest fails to load.
type versionInfo struct {
	Version  string        `json:"version"`
	Manifest *manifestInfo `json:"manifest"`
}

// Styled renders the version and manifest provenance as plain text.
func (v versionInfo) Styled() string {
	s := "lk " + v.Version + "\n"
	if v.Manifest == nil {
		return s + "manifest: (unavailable)\n"
	}
	return s + "manifest: " + v.Manifest.GeneratedAt + " (" + v.Manifest.Source + ")\n"
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := versionInfo{Version: version}
			if m, err := loadManifest(); err == nil {
				info.Manifest = &manifestInfo{GeneratedAt: m.GeneratedAt, Source: m.Source}
			}
			return output.Render(cmd.OutOrStdout(), formatFlag(cmd), info)
		},
	}
}
