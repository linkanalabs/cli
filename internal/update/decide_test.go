package update

import "testing"

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
