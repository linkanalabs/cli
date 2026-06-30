// Package commands implements the lk CLI command tree.
package commands

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/output"
)

// version is the binary version, set by SetVersion from main.
var version = "dev"

// errSilent signals a non-zero exit without printing an extra error line
// (the command already rendered its own output).
var errSilent = errors.New("")

// SetVersion sets the version reported by the CLI.
func SetVersion(v string) {
	if v != "" {
		version = v
	}
}

// newRootCmd builds a fresh command tree. A constructor (rather than a package
// global) keeps tests isolated from each other.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "lk",
		Short:         "Linkana CLI",
		Long:          "lk is the command-line interface for Linkana.",
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.PersistentFlags().String("format", output.FormatAuto, "output format: auto|json|styled")
	root.AddCommand(newVersionCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newAuthCmd())
	root.AddCommand(newWhoamiCmd())
	root.AddCommand(newSupplierCmd())
	root.AddCommand(newImpersonateCmd())
	root.AddCommand(newModeCmd())
	return root
}

// run executes the CLI with the given args and streams, returning an exit code.
// It is the testable core of Execute.
func run(args []string, stdout, stderr io.Writer) int {
	return runWith(os.Stdin, args, stdout, stderr)
}

// runWith is like run but with an explicit stdin, so commands that read from
// stdin (e.g. auth login prompt) stay testable.
func runWith(stdin io.Reader, args []string, stdout, stderr io.Writer) int {
	root := newRootCmd()
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)

	if err := root.Execute(); err != nil {
		if !errors.Is(err, errSilent) {
			_, _ = fmt.Fprintln(stderr, "error:", err)
		}
		return 1
	}
	return 0
}

// Execute runs the CLI and exits the process with the appropriate code.
func Execute() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// formatFlag returns the resolved --format value for a command.
func formatFlag(cmd *cobra.Command) string {
	f, _ := cmd.Flags().GetString("format")
	return f
}
