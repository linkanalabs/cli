package commands

import (
	"fmt"
	"net/url"
	"os"

	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/config"
	"github.com/linkanalabs/cli/internal/output"
)

type configView struct {
	BaseURL string `json:"base_url"`
	Source  string `json:"source"` // env | file | default
	Path    string `json:"config_path"`
}

func (v configView) Styled() string {
	return fmt.Sprintf("base_url: %s (source: %s)\nconfig:   %s\n", v.BaseURL, v.Source, v.Path)
}

// resolveConfigView reports the effective base_url and where it came from,
// mirroring the precedence in config.Load (env > file > default).
func resolveConfigView() (configView, error) {
	path, err := config.Path()
	if err != nil {
		return configView{}, err
	}
	cfg, err := config.Load()
	if err != nil {
		return configView{}, err
	}

	source := "default"
	if os.Getenv(config.EnvBaseURL) != "" {
		source = "env"
	} else {
		fileURL, err := config.FileBaseURL()
		if err != nil {
			return configView{}, err
		}
		if fileURL != "" {
			source = "file"
		}
	}
	return configView{BaseURL: cfg.BaseURL, Source: source, Path: path}, nil
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or set the CLI configuration (base_url)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			v, err := resolveConfigView()
			if err != nil {
				return err
			}
			return output.Render(cmd.OutOrStdout(), formatFlag(cmd), v)
		},
	}
	cmd.AddCommand(newConfigSetURLCmd())
	return cmd
}

func newConfigSetURLCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-url <url>",
		Short: "Write the backend base URL to the config file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw := args[0]
			if err := validateBaseURL(raw); err != nil {
				return err
			}

			cfg, err := config.Load()
			if err != nil {
				return err
			}
			cfg.BaseURL = raw
			if err := cfg.Save(); err != nil {
				return err
			}

			if env := os.Getenv(config.EnvBaseURL); env != "" {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
					"warning: %s=%s is set and overrides the config file at runtime\n", config.EnvBaseURL, env)
			}

			path, err := config.Path()
			if err != nil {
				return err
			}
			return output.Render(cmd.OutOrStdout(), formatFlag(cmd),
				configView{BaseURL: raw, Source: "file", Path: path})
		},
	}
}

func validateBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("invalid URL %q: expected http(s)://host", raw)
	}
	return nil
}
