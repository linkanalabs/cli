package update

import "fmt"

// InstallScriptCommand reinstalls lk outside Homebrew. It is the same command
// the README documents for a first install: the script resolves the newest
// release and verifies the archive's sha256 against checksums.txt.
const InstallScriptCommand = "curl -fsSL https://raw.githubusercontent.com/linkanalabs/cli/main/scripts/install.sh | sh"

// BrewUpgradeCommand upgrades only lk. It deliberately does not run
// `brew update` first: that has no per-tap scope (see `brew update --help`,
// which fetches all formulae) and would refresh every unrelated tap on the
// machine. Brew still runs its own auto-update on its own schedule, exactly as
// it would if the command were typed by hand.
const BrewUpgradeCommand = "brew upgrade --cask lk"

// BrewRefreshCommand is what a person has to run when brew's metadata trails
// the tap. lk suggests it and never runs it unprompted.
const BrewRefreshCommand = "brew update && " + BrewUpgradeCommand

// Inputs are the versions a decision needs. TapRemote, TapLocal and Release are
// empty when they could not be resolved.
type Inputs struct {
	Install   *Install
	Current   string
	TapRemote string
	TapLocal  string
	Release   string
}

// Decision is the outcome: what is installable and how.
type Decision struct {
	UpdateAvailable bool
	// CanSelfUpgrade reports whether lk may install the update itself. It is
	// false both outside Homebrew and when brew's metadata still trails the
	// tap; in either case Command says what a person should run instead.
	CanSelfUpgrade bool
	// Latest is the newest *installable* version, which is the tap's — not
	// necessarily the newest published release.
	Latest  string
	Command string
	Warning string
}

// Decide applies the version precedence. The tap, not the GitHub release, is
// the source of truth for what can be installed: they diverge when a release
// ships and the tap commit fails, and pointing someone at `brew upgrade` for a
// version brew does not have only generates support.
func Decide(in Inputs) Decision {
	d := Decision{Latest: in.TapRemote}

	// Either ordering between the tap and the published release is an anomaly in
	// the release pipeline, and both are worth surfacing even when nothing is
	// upgradable: behind is the documented tap-commit failure, ahead means the
	// tap carries something that was never published as the latest release.
	if in.Release != "" && in.TapRemote != "" {
		switch {
		case Newer(in.Release, in.TapRemote):
			d.Warning = fmt.Sprintf(
				"release %s is published but the tap still serves %s; brew cannot install it yet",
				in.Release, in.TapRemote)
		case Newer(in.TapRemote, in.Release):
			d.Warning = fmt.Sprintf(
				"the tap serves %s, ahead of the published release %s",
				in.TapRemote, in.Release)
		}
	}

	if !Newer(in.TapRemote, in.Current) {
		return d
	}
	d.UpdateAvailable = true

	switch {
	case in.Install == nil || !in.Install.Homebrew():
		d.Command = InstallScriptCommand
	// No readable local cask means Homebrew's own receipt could not be
	// confirmed, and a path can be forged. Never hand brew a binary it cannot
	// be shown to have installed: say what to run instead.
	case in.TapLocal == "":
		d.Command = BrewUpgradeCommand
	// GoReleaser commits the cask for prerelease tags too (no skip_upload), and
	// /releases/latest skips prereleases — so a `vX.Y.Z-rc.1` tag makes the tap
	// serve a release candidate while the published latest stays stable. Never
	// move a stable installation onto one unprompted; say so and let the person
	// decide.
	case Prerelease(in.TapRemote) && !Prerelease(in.Current):
		d.Warning = fmt.Sprintf(
			"%s is a prerelease; lk will not install it on its own", in.TapRemote)
		d.Command = BrewUpgradeCommand
	case Newer(in.TapRemote, in.TapLocal):
		d.Command = BrewRefreshCommand
	default:
		d.CanSelfUpgrade = true
		d.Command = BrewUpgradeCommand
	}
	return d
}
