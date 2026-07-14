package commands

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/config"
	"github.com/linkanalabs/cli/internal/mode"
	"github.com/linkanalabs/cli/internal/output"
)

// isStdinTTY is a seam so tests can simulate a (non-)terminal stdin.
var isStdinTTY = func() bool { return isatty.IsTerminal(os.Stdin.Fd()) }

type modeView struct {
	Origin string    `json:"origin"`
	Mode   mode.Mode `json:"mode"`
}

func (v modeView) Styled() string {
	return fmt.Sprintf("mode: %s @ %s\n", v.Mode, v.Origin)
}

func activeOrigin() (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	return cfg.BaseURL, nil
}

func newModeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mode",
		Short: "Show or set the read/write mode for the active origin",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			origin, err := activeOrigin()
			if err != nil {
				return err
			}
			m, err := mode.Load(origin)
			if err != nil {
				return err
			}
			return output.Render(cmd.OutOrStdout(), formatFlag(cmd), modeView{Origin: origin, Mode: m})
		},
	}
	cmd.AddCommand(newModeWriteCmd(), newModeReadCmd())
	return cmd
}

func newModeWriteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "write",
		Short: "Enable writes for the active origin (requires TTY confirmation)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			origin, err := activeOrigin()
			if err != nil {
				return err
			}
			if !isStdinTTY() {
				return fmt.Errorf("enabling write requires interactive confirmation (no TTY)")
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Enable WRITE on %s? type \"write\" to confirm: ", origin)
			line, _ := readLine(cmd.InOrStdin())
			if strings.TrimSpace(line) != "write" {
				return fmt.Errorf("confirmation mismatch; mode unchanged")
			}
			if err := mode.Save(origin, mode.Write); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "write enabled on %s\n", origin)
			return nil
		},
	}
}

func newModeReadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "read",
		Short: "Return the active origin to read mode (no confirmation)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			origin, err := activeOrigin()
			if err != nil {
				return err
			}
			if err := mode.Save(origin, mode.Read); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "read mode on %s\n", origin)
			return nil
		},
	}
}

func readLine(r io.Reader) (string, error) {
	var b strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				break
			}
			b.WriteByte(buf[0])
		}
		if err != nil {
			break
		}
	}
	return b.String(), nil
}
