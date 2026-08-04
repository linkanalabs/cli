package update

import (
	"strings"
	"testing"
)

func caskInstall() *Install {
	return &Install{Method: MethodHomebrewCask, resolved: "/opt/homebrew/Caskroom/lk/0.7.0/lk"}
}

func otherInstall() *Install {
	return &Install{Method: MethodOther, resolved: "/home/cirdes/.local/bin/lk"}
}

func TestDecideUpToDate(t *testing.T) {
	got := Decide(Inputs{Install: caskInstall(), Current: "0.7.0", TapRemote: "0.7.0", TapLocal: "0.7.0"})

	if got.UpdateAvailable {
		t.Error("UpdateAvailable = true for a current install")
	}
	if got.CanSelfUpgrade {
		t.Error("CanSelfUpgrade = true with nothing to install")
	}
}

// A local build reports "dev"; it must never be told it is out of date, or
// every developer would get an upgrade prompt on every command.
func TestDecideDevBuildIsNeverStale(t *testing.T) {
	got := Decide(Inputs{Install: caskInstall(), Current: "dev", TapRemote: "9.9.9", TapLocal: "9.9.9"})

	if got.UpdateAvailable || got.CanSelfUpgrade {
		t.Errorf("dev build produced UpdateAvailable=%v CanSelfUpgrade=%v", got.UpdateAvailable, got.CanSelfUpgrade)
	}
}

// Brew already has the new cask on disk, so a plain upgrade will work — and it
// must be a plain upgrade, never a global `brew update`, which would refresh
// every unrelated tap on the machine.
func TestDecideBrewCanUpgrade(t *testing.T) {
	got := Decide(Inputs{Install: caskInstall(), Current: "0.7.0", TapRemote: "0.8.0", TapLocal: "0.8.0"})

	if !got.UpdateAvailable {
		t.Fatal("UpdateAvailable = false with a newer version published")
	}
	if !got.CanSelfUpgrade {
		t.Error("CanSelfUpgrade = false when brew already has the cask")
	}
	if got.Command != "brew upgrade --cask lk" {
		t.Errorf("Command = %q", got.Command)
	}
}

// Brew's metadata trails the tap, so `brew upgrade` alone would silently do
// nothing. Refreshing it is the user's call, not ours.
func TestDecideBrewMetadataStale(t *testing.T) {
	got := Decide(Inputs{Install: caskInstall(), Current: "0.7.0", TapRemote: "0.8.0", TapLocal: "0.7.0"})

	if !got.UpdateAvailable {
		t.Fatal("UpdateAvailable = false with a newer version published")
	}
	if got.CanSelfUpgrade {
		t.Error("CanSelfUpgrade = true against stale brew metadata")
	}
	if got.Command != "brew update && brew upgrade --cask lk" {
		t.Errorf("Command = %q", got.Command)
	}
}

// Without a readable local cask, Homebrew's own receipt could not be confirmed
// — and a Caskroom-shaped path can be forged. brew must not be handed a binary
// it cannot be shown to have installed; the command is reported instead.
func TestDecideUnconfirmedInstallIsNotSelfUpgraded(t *testing.T) {
	got := Decide(Inputs{Install: caskInstall(), Current: "0.7.0", TapRemote: "0.8.0", TapLocal: ""})

	if !got.UpdateAvailable {
		t.Fatal("UpdateAvailable = false with a newer version published")
	}
	if got.CanSelfUpgrade {
		t.Error("CanSelfUpgrade = true without a confirmed Homebrew receipt")
	}
	if got.Command != BrewUpgradeCommand {
		t.Errorf("Command = %q, want the brew upgrade command", got.Command)
	}
}

func TestDecideNonHomebrewIsManual(t *testing.T) {
	got := Decide(Inputs{Install: otherInstall(), Current: "0.7.0", TapRemote: "0.8.0"})

	if !got.UpdateAvailable {
		t.Fatal("UpdateAvailable = false with a newer version published")
	}
	if got.CanSelfUpgrade {
		t.Error("CanSelfUpgrade = true outside Homebrew")
	}
	if got.Command != InstallScriptCommand {
		t.Errorf("Command = %q, want the install.sh command", got.Command)
	}
}

// The tap was rolled back, or the local clone kept something since removed.
// brew would install what the clone has, not the version just reported — which
// is how a yanked release or a refused prerelease could come back.
func TestDecideLocalCaskAheadOfTapIsNotSelfUpgraded(t *testing.T) {
	got := Decide(Inputs{
		Install: caskInstall(), Current: "0.7.0",
		TapRemote: "0.8.0", TapLocal: "0.9.0",
	})

	if !got.UpdateAvailable {
		t.Fatal("UpdateAvailable = false; the tap does serve something newer than the install")
	}
	if got.CanSelfUpgrade {
		t.Error("CanSelfUpgrade = true with brew holding a version ahead of the tap")
	}
	if got.Command != BrewRefreshCommand {
		t.Errorf("Command = %q, want the refresh command", got.Command)
	}
	if !strings.Contains(got.Warning, "ahead of the tap") {
		t.Errorf("Warning = %q", got.Warning)
	}
}

// Anything brew did not install as a cask cannot be self-upgraded: the upgrade
// runs `brew upgrade --cask lk`, which would fail against it.
func TestDecideOnlyCasksSelfUpgrade(t *testing.T) {
	got := Decide(Inputs{Install: otherInstall(), Current: "0.7.0", TapRemote: "0.8.0", TapLocal: "0.8.0"})

	if got.CanSelfUpgrade {
		t.Error("CanSelfUpgrade = true outside a cask install")
	}
	if got.Command != InstallScriptCommand {
		t.Errorf("Command = %q", got.Command)
	}
}

// A `vX.Y.Z-rc.1` tag makes GoReleaser commit the cask (there is no skip_upload)
// while /releases/latest keeps pointing at the last stable one. Auto-upgrading
// on that would move the whole fleet onto a release candidate.
func TestDecideNeverSelfUpgradesOntoAPrerelease(t *testing.T) {
	got := Decide(Inputs{
		Install: caskInstall(), Current: "0.8.0",
		TapRemote: "0.9.0-rc.1", TapLocal: "0.9.0-rc.1", Release: "v0.8.0",
	})

	if !got.UpdateAvailable {
		t.Error("UpdateAvailable = false; the tap really does serve something newer")
	}
	if got.CanSelfUpgrade {
		t.Fatal("CanSelfUpgrade = true onto a prerelease")
	}
	if got.Warning == "" {
		t.Error("no warning explaining the refusal")
	}
	if got.Command != BrewUpgradeCommand {
		t.Errorf("Command = %q, want the plain brew upgrade so a person can opt in", got.Command)
	}
}

// A refusal to install must not swallow the report that the pipeline is broken:
// the tap trailing the published release is a real failure, and it stays visible
// even when the version on offer is a prerelease lk will not install.
func TestDecideKeepsBothWarnings(t *testing.T) {
	got := Decide(Inputs{
		Install: caskInstall(), Current: "0.8.0",
		TapRemote: "0.9.0-rc.1", TapLocal: "0.9.0-rc.1", Release: "v1.0.0",
	})

	if !strings.Contains(got.Warning, "still serves") {
		t.Errorf("the pipeline anomaly was lost: %q", got.Warning)
	}
	if !strings.Contains(got.Warning, "prerelease") {
		t.Errorf("the prerelease refusal was lost: %q", got.Warning)
	}
}

// Right after a release candidate is published, brew's local metadata is behind
// by definition — so whoever opts in must be given the command that refreshes it
// first, or it is a silent no-op.
func TestDecidePrereleaseWithStaleMetadata(t *testing.T) {
	got := Decide(Inputs{
		Install: caskInstall(), Current: "0.8.0",
		TapRemote: "0.9.0-rc.1", TapLocal: "0.8.0",
	})

	if got.CanSelfUpgrade {
		t.Error("CanSelfUpgrade = true onto a prerelease")
	}
	if got.Command != BrewRefreshCommand {
		t.Errorf("Command = %q, want the refresh command; a plain upgrade would do nothing", got.Command)
	}
}

// Already on a prerelease, moving to the next one is not a surprise.
func TestDecideSelfUpgradesBetweenPrereleases(t *testing.T) {
	got := Decide(Inputs{
		Install: caskInstall(), Current: "0.9.0-rc.1",
		TapRemote: "0.9.0-rc.2", TapLocal: "0.9.0-rc.2",
	})

	if !got.CanSelfUpgrade {
		t.Error("CanSelfUpgrade = false between prereleases")
	}
}

// A tap ahead of the published release is an anomaly in the pipeline, the
// mirror of the documented tap-behind case.
func TestDecideWarnsWhenTapIsAheadOfTheRelease(t *testing.T) {
	got := Decide(Inputs{
		Install: caskInstall(), Current: "0.8.0",
		TapRemote: "0.9.0", TapLocal: "0.9.0", Release: "v0.8.0",
	})

	if got.Warning == "" {
		t.Fatal("expected a warning when the tap runs ahead of the published release")
	}
	if !strings.Contains(got.Warning, "ahead") {
		t.Errorf("Warning = %q", got.Warning)
	}
}

// The documented pitfall: the release shipped but the tap commit did not, so
// brew would keep serving the old version silently.
func TestDecideWarnsWhenTapTrailsTheRelease(t *testing.T) {
	got := Decide(Inputs{
		Install: caskInstall(), Current: "0.7.0",
		TapRemote: "0.7.0", TapLocal: "0.7.0", Release: "v0.8.0",
	})

	if got.Warning == "" {
		t.Fatal("expected a warning when the tap trails the published release")
	}
	// The tap is what brew can install, so there is still nothing to upgrade to.
	if got.CanSelfUpgrade {
		t.Error("CanSelfUpgrade = true with nothing to install")
	}
}

func TestDecideNoWarningWhenTapMatchesRelease(t *testing.T) {
	got := Decide(Inputs{
		Install: caskInstall(), Current: "0.7.0",
		TapRemote: "0.8.0", TapLocal: "0.8.0", Release: "v0.8.0",
	})

	if got.Warning != "" {
		t.Errorf("unexpected warning: %q", got.Warning)
	}
}

// Nothing was fetched (offline, or the caller only wanted a cheap check).
func TestDecideWithoutRemoteVersion(t *testing.T) {
	got := Decide(Inputs{Install: caskInstall(), Current: "0.7.0"})

	if got.UpdateAvailable || got.CanSelfUpgrade {
		t.Error("expected no action without a remote version")
	}
}

// Decide is called from the automatic path, where a nil install would mean
// detection failed. It must not panic.
func TestDecideWithoutInstall(t *testing.T) {
	got := Decide(Inputs{Current: "0.7.0", TapRemote: "0.8.0"})

	if got.CanSelfUpgrade || got.Command != InstallScriptCommand {
		t.Errorf("a nil install must fall back to manual, got %+v", got)
	}
}

func TestDecideLatestReportsTheInstallableVersion(t *testing.T) {
	got := Decide(Inputs{Install: caskInstall(), Current: "0.7.0", TapRemote: "0.8.0", TapLocal: "0.8.0"})

	if got.Latest != "0.8.0" {
		t.Errorf("Latest = %q, want 0.8.0", got.Latest)
	}
}
