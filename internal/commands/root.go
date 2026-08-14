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
		Use:   "lk",
		Short: "Linkana CLI",
		Long: "lk is the command-line interface for Linkana.\n\n" +
			"Output formats (--format):\n" +
			"  auto      styled on a terminal, json when piped (default)\n" +
			"  json      the stable contract: the response, pretty-printed\n" +
			"  styled    aligned table or key/value block, for a human\n" +
			"  markdown  GFM table or bold labels, to paste into a document\n" +
			"  ids       one id per line, to feed the next command\n" +
			"  count     how many records this response carries",
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.PersistentFlags().Var(&formatValue{value: output.FormatAuto}, "format", "output format: "+output.FormatList())
	root.AddCommand(newVersionCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newAuthCmd())
	root.AddCommand(newWhoamiCmd())
	root.AddCommand(newImpersonateCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newUpdateCmd())
	root.AddCommand(newCertificateListsCmds())
	// Dynamic (manifest-driven) commands mount last so manual commands always
	// win name collisions. A manifest load failure disables them; `lk version`
	// surfaces the manifest state.
	if m, err := loadManifest(); err == nil {
		registerDynamic(root, m)
	}
	return root
}

// run executes the CLI with the given args and streams, returning an exit code.
// It is the testable core of Execute.
func run(args []string, stdout, stderr io.Writer) int {
	code, _ := runWith(os.Stdin, args, stdout, stderr)
	return code
}

// runWith is like run but with an explicit stdin, so commands that read from
// stdin (e.g. auth login prompt) stay testable. It also reports which command
// cobra actually ran, which Execute needs and run() discards.
func runWith(stdin io.Reader, args []string, stdout, stderr io.Writer) (int, *cobra.Command) {
	root := newRootCmd()
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)

	// ExecuteC (rather than Execute) reports which command actually ran, which
	// the auto-update guard needs: parsing args would mistake a flag value for
	// a command name.
	executed, err := root.ExecuteC()

	if err != nil {
		if !errors.Is(err, errSilent) {
			_, _ = fmt.Fprintln(stderr, "error:", err)
		}
		return 1, executed
	}
	return 0, executed
}

// Execute runs the CLI and exits the process with the appropriate code.
func Execute() {
	code, executed := runWith(os.Stdin, os.Args[1:], os.Stdout, os.Stderr)

	// Last thing before the process exits, and deliberately here rather than in
	// runWith: reaching the network, writing state and forking a detached brew
	// are effects of the binary having really run, not of executing the command
	// tree. Keeping them out of run() means no test trips them by accident. The
	// output is already written, so a new version can only apply from the next
	// invocation anyway. A no-op unless a day has passed on a Homebrew install.
	maybeAutoUpdate(os.Stderr, executed)

	os.Exit(code)
}

// formatFlag returns the resolved --format value for a command.
func formatFlag(cmd *cobra.Command) string {
	f, _ := cmd.Flags().GetString("format")
	return f
}

// formatValue validates --format while pflag parses it, so an unknown value
// fails before any command body runs: no request is spent on a typo, and no
// write happens only to fail at render time. Validating at parse time (rather
// than in a PersistentPreRunE) also covers `lk --format x` with no subcommand
// and cannot be shadowed by a hook on a future subcommand.
type formatValue struct{ value string }

func (f *formatValue) String() string { return f.value }

func (f *formatValue) Set(v string) error {
	if !output.Valid(v) {
		return fmt.Errorf("unknown format %q: valid values are %s", v, output.FormatList())
	}
	f.value = v
	return nil
}

// Type reports "string" so cmd.Flags().GetString("format") keeps working and
// the help still reads `--format string`.
func (f *formatValue) Type() string { return "string" }
