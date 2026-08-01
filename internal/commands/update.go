package commands

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/output"
	"github.com/linkanalabs/cli/internal/state"
	"github.com/linkanalabs/cli/internal/update"
)

// Seams for everything that touches the machine or the network, so the command
// is exercised without a Homebrew install and without leaving the test process.
var (
	detectInstall  = update.Detect
	fetchTapRemote = update.TapRemote
	readTapLocal   = update.TapLocal
	fetchRelease   = update.Release
	runUpgrade     = update.RunUpgrade
	upgradeLogPath = state.UpgradeLogPath
)

// tapLocalFor resolves the version brew already has on disk. Every caller
// treats "could not tell" the same way brew does — try anyway — so the error is
// folded into an empty string once, here.
func tapLocalFor(in *update.Install) string {
	v, err := readTapLocal(in.CaskPath())
	if err != nil {
		return ""
	}
	return v
}

const (
	// updateHTTPTimeout bounds the lookup that decides what to install.
	updateHTTPTimeout = 10 * time.Second

	// releaseProbeTimeout bounds the release lookup, which only sharpens a
	// warning. It gets less patience on purpose: the actionable version is
	// already known by then, and an optional signal must never be able to
	// postpone an upgrade someone asked for.
	releaseProbeTimeout = 2 * time.Second
)

// updateResult is the contract. latest_version is what can be installed right
// now (the tap's); release_version is what has been published. They differ when
// a release ships and the tap commit fails, and only the first one is
// actionable.
type updateResult struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	ReleaseVersion  string `json:"release_version,omitempty"`
	InstallMethod   string `json:"install_method"`
	UpdateAvailable bool   `json:"update_available"`
	UpgradeCommand  string `json:"upgrade_command,omitempty"`
	Upgraded        bool   `json:"upgraded"`
	Warning         string `json:"warning,omitempty"`
}

// Styled renders the diagnostic view. Like version, doctor and config, this is
// a bespoke layout rather than a resource, so it carries its own styler.
func (r updateResult) Styled() string {
	s := "lk " + r.CurrentVersion + " (" + r.InstallMethod + ")\n"
	switch {
	case r.Upgraded:
		s += "upgraded to " + r.LatestVersion + "\n"
	case r.UpdateAvailable:
		s += "update available: " + r.LatestVersion + "\n"
		if r.UpgradeCommand != "" {
			s += "run: " + r.UpgradeCommand + "\n"
		}
	default:
		s += "up to date (latest " + r.LatestVersion + ")\n"
	}
	if r.Warning != "" {
		s += "warning: " + r.Warning + "\n"
	}
	return s
}

// probeRelease resolves the newest published release on a short leash, and
// gives up quietly: it feeds a warning, never the decision.
func probeRelease(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, releaseProbeTimeout)
	defer cancel()

	v, err := fetchRelease(ctx, &http.Client{Timeout: releaseProbeTimeout})
	if err != nil {
		return ""
	}
	return v
}

func newUpdateCmd() *cobra.Command {
	var check bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update lk to the newest published version",
		Long: "Update lk to the newest published version.\n\n" +
			"Installed via Homebrew, this runs `brew upgrade --cask lk`. It never runs a\n" +
			"bare `brew update`: that has no per-tap scope and would refresh every\n" +
			"unrelated tap on the machine. When brew's own metadata trails the tap, lk\n" +
			"reports the command to run instead of running it.\n\n" +
			"Installed any other way (scripts/install.sh, go install), lk cannot replace\n" +
			"itself and prints the reinstall command instead.\n\n" +
			"Exit codes:\n" +
			"  0  already current, or upgraded successfully\n" +
			"  1  an upgrade was attempted and did not happen (the output says why and how)\n\n" +
			"--check is a read and always exits 0 when the lookup succeeded; the answer\n" +
			"is the update_available field, not the exit code.\n\n" +
			"lk also checks for updates on its own, at most once a day; set\n" +
			"LK_NO_AUTO_UPDATE to turn that off.",
		Args: cobra.NoArgs,
		// This command updates explicitly and synchronously; the automatic
		// check must stand down rather than start a second brew behind it.
		Annotations: map[string]string{annotationNoAutoUpdate: "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			in, err := detectInstall()
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			hc := &http.Client{Timeout: updateHTTPTimeout}

			// Not knowing the published version is a failure: reporting "up to
			// date" without having looked would be a lie.
			tapRemote, err := fetchTapRemote(ctx, hc)
			if err != nil {
				return fmt.Errorf("resolving the published version: %w", err)
			}
			// These only sharpen the answer, so losing them is not fatal: the
			// local cask says whether brew can act, the release says whether
			// the tap is behind.
			release := probeRelease(ctx)

			d := update.Decide(update.Inputs{
				Install:   in,
				Current:   version,
				TapRemote: tapRemote,
				TapLocal:  tapLocalFor(in),
				Release:   release,
			})

			res := updateResult{
				CurrentVersion:  version,
				LatestVersion:   d.Latest,
				ReleaseVersion:  release,
				InstallMethod:   string(in.Method),
				UpdateAvailable: d.UpdateAvailable,
				UpgradeCommand:  d.Command,
				Warning:         d.Warning,
			}

			render := func() error { return output.Render(cmd.OutOrStdout(), formatFlag(cmd), res) }

			if check || !d.UpdateAvailable {
				return render()
			}

			if d.CanSelfUpgrade {
				// brew's own output belongs on stderr; stdout stays the contract.
				upgradeErr := runUpgrade(ctx, cmd.ErrOrStderr())
				res.Upgraded = upgradeErr == nil
				// Render either way: what was available and what was attempted is
				// worth the same to a caller whether or not brew succeeded, and
				// an empty stdout would be indistinguishable from no response.
				if err := render(); err != nil {
					return err
				}
				return upgradeErr
			}

			// Stale brew metadata or a non-Homebrew install: lk cannot do it.
			// Report the data anyway, say how, and exit non-zero — the update
			// the caller asked for did not happen.
			if err := render(); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "lk %s is available; run: %s\n", d.Latest, d.Command)
			return errSilent
		},
	}

	cmd.Flags().BoolVar(&check, "check", false, "report what is available without installing anything")
	return cmd
}
