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

	// The published release running ahead of the tap is the documented failure
	// mode of the release pipeline; surface it even when nothing is upgradable.
	if in.Release != "" && in.TapRemote != "" && Newer(in.Release, in.TapRemote) {
		d.Warning = fmt.Sprintf(
			"release %s is published but the tap still serves %s; brew cannot install it yet",
			in.Release, in.TapRemote)
	}

	if !Newer(in.TapRemote, in.Current) {
		return d
	}
	d.UpdateAvailable = true

	switch {
	case in.Install == nil || !in.Install.Homebrew():
		d.Command = InstallScriptCommand
	// An unreadable local cask leaves brew's freshness unknown; trying is the
	// safe default, since the worst case is brew reporting nothing to do.
	case in.TapLocal != "" && Newer(in.TapRemote, in.TapLocal):
		d.Command = BrewRefreshCommand
	default:
		d.CanSelfUpgrade = true
		d.Command = BrewUpgradeCommand
	}
	return d
}
