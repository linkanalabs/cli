package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/linkanalabs/cli/internal/update"
)

// updateStubs replaces everything the update command touches outside itself.
type updateStubs struct {
	install   *update.Install
	installEr error
	tapRemote string
	tapRemErr error
	tapLocal  string
	tapLocErr error
	release   string
	releaseEr error
	upgradeEr error

	upgrades int // how many times an upgrade was actually attempted
}

// swap replaces a package-level seam for the duration of a test. Keeping the
// save and the restore in one call is what stops a forgotten restore from
// leaking into the rest of the package.
func swap[T any](t *testing.T, seam *T, v T) {
	t.Helper()
	prev := *seam
	*seam = v
	t.Cleanup(func() { *seam = prev })
}

func (s *updateStubs) apply(t *testing.T) {
	t.Helper()
	swap(t, &detectInstall, func() (*update.Install, error) { return s.install, s.installEr })
	swap(t, &fetchTapRemote, func(context.Context, *http.Client) (string, error) { return s.tapRemote, s.tapRemErr })
	swap(t, &readTapLocal, func(string) (string, error) { return s.tapLocal, s.tapLocErr })
	swap(t, &fetchRelease, func(context.Context, *http.Client) (string, error) { return s.release, s.releaseEr })
	swap(t, &runUpgrade, func(context.Context, io.Writer) error {
		s.upgrades++
		return s.upgradeEr
	})
}

// caskAt is a Homebrew cask install. The cask path it would report comes from
// readTapLocal, which every test stubs, so nothing here needs a real receipt.
func caskAt(string) *update.Install {
	return &update.Install{Method: update.MethodHomebrewCask}
}

func withVersion(t *testing.T, v string) {
	t.Helper()
	prev := version
	version = v
	t.Cleanup(func() { version = prev })
}

func TestUpdateCheckUpToDate(t *testing.T) {
	withVersion(t, "0.7.0")
	s := &updateStubs{install: caskAt("0.7.0"), tapRemote: "0.7.0", tapLocal: "0.7.0", release: "v0.7.0"}
	s.apply(t)

	var out, errOut bytes.Buffer
	code := run([]string{"update", "--check", "--format", "json"}, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"update_available": false`) {
		t.Errorf("output = %q", out.String())
	}
	if s.upgrades != 0 {
		t.Errorf("ran %d upgrades on an up-to-date install", s.upgrades)
	}
}

// --check is a read: it reports and never mutates, whatever it finds.
func TestUpdateCheckNeverUpgrades(t *testing.T) {
	withVersion(t, "0.7.0")
	s := &updateStubs{install: caskAt("0.7.0"), tapRemote: "0.8.0", tapLocal: "0.8.0"}
	s.apply(t)

	var out, errOut bytes.Buffer
	code := run([]string{"update", "--check", "--format", "json"}, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}
	if s.upgrades != 0 {
		t.Fatalf("--check ran %d upgrades", s.upgrades)
	}
	body := out.String()
	for _, want := range []string{
		`"update_available": true`,
		`"latest_version": "0.8.0"`,
		`"upgrade_command": "brew upgrade --cask lk"`,
		`"upgraded": false`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("output missing %q: %q", want, body)
		}
	}
}

func TestUpdateUpgradesViaBrew(t *testing.T) {
	withVersion(t, "0.7.0")
	s := &updateStubs{install: caskAt("0.7.0"), tapRemote: "0.8.0", tapLocal: "0.8.0"}
	s.apply(t)

	var out, errOut bytes.Buffer
	code := run([]string{"update", "--format", "json"}, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}
	if s.upgrades != 1 {
		t.Fatalf("ran %d upgrades, want 1", s.upgrades)
	}
	if !strings.Contains(out.String(), `"upgraded": true`) {
		t.Errorf("output = %q", out.String())
	}
}

func TestUpdateUpgradeFailureExitsOne(t *testing.T) {
	withVersion(t, "0.7.0")
	s := &updateStubs{
		install: caskAt("0.7.0"), tapRemote: "0.8.0", tapLocal: "0.8.0",
		upgradeEr: errors.New("brew blew up"),
	}
	s.apply(t)

	var out, errOut bytes.Buffer
	if code := run([]string{"update", "--format", "json"}, &out, &errOut); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "brew blew up") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

// Brew's metadata trails the tap, so upgrading would silently do nothing.
// Refreshing every tap on the machine is the user's call, so lk only says so.
func TestUpdateBrewMetadataStale(t *testing.T) {
	withVersion(t, "0.7.0")
	s := &updateStubs{install: caskAt("0.7.0"), tapRemote: "0.8.0", tapLocal: "0.7.0"}
	s.apply(t)

	var out, errOut bytes.Buffer
	code := run([]string{"update", "--format", "json"}, &out, &errOut)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (nothing was upgraded)", code)
	}
	if s.upgrades != 0 {
		t.Fatalf("ran %d upgrades against stale metadata", s.upgrades)
	}
	// Decode rather than match raw text: the encoder escapes "&" as "&",
	// which decodes to the same string for any consumer.
	var got updateResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decoding output: %v (%q)", err, out.String())
	}
	if got.UpgradeCommand != "brew update && brew upgrade --cask lk" {
		t.Errorf("UpgradeCommand = %q", got.UpgradeCommand)
	}
}

// Outside Homebrew lk cannot replace itself; it says how, and reports failure
// so a caller can tell the update did not happen.
func TestUpdateNonHomebrewInstructs(t *testing.T) {
	withVersion(t, "0.7.0")
	s := &updateStubs{install: &update.Install{Method: update.MethodOther}, tapRemote: "0.8.0"}
	s.apply(t)

	var out, errOut bytes.Buffer
	code := run([]string{"update", "--format", "json"}, &out, &errOut)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if s.upgrades != 0 {
		t.Fatalf("ran %d upgrades on a non-brew install", s.upgrades)
	}
	if !strings.Contains(out.String(), `"install_method": "other"`) {
		t.Errorf("stdout = %q", out.String())
	}
	if !strings.Contains(errOut.String(), update.InstallScriptCommand) {
		t.Errorf("stderr should carry the reinstall command, got %q", errOut.String())
	}
}

// The documented release pitfall: the release shipped, the tap commit did not.
func TestUpdateWarnsWhenTapTrailsRelease(t *testing.T) {
	withVersion(t, "0.7.0")
	s := &updateStubs{install: caskAt("0.7.0"), tapRemote: "0.7.0", tapLocal: "0.7.0", release: "v0.8.0"}
	s.apply(t)

	var out, errOut bytes.Buffer
	code := run([]string{"update", "--check", "--format", "json"}, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}
	body := out.String()
	if !strings.Contains(body, `"warning"`) || !strings.Contains(body, "tap still serves") {
		t.Errorf("expected a tap-lag warning, got %q", body)
	}
	if !strings.Contains(body, `"release_version": "v0.8.0"`) {
		t.Errorf("output = %q", body)
	}
}

// A development build has no version to compare, so it is never stale.
func TestUpdateDevBuild(t *testing.T) {
	withVersion(t, "dev")
	s := &updateStubs{install: caskAt("0.7.0"), tapRemote: "9.9.9", tapLocal: "9.9.9"}
	s.apply(t)

	var out, errOut bytes.Buffer
	code := run([]string{"update", "--format", "json"}, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}
	if s.upgrades != 0 {
		t.Fatalf("ran %d upgrades on a dev build", s.upgrades)
	}
}

// Not knowing the published version is a failure: silently reporting "up to
// date" would be a lie.
func TestUpdateTapUnreachable(t *testing.T) {
	withVersion(t, "0.7.0")
	s := &updateStubs{install: caskAt("0.7.0"), tapRemErr: errors.New("dns is down")}
	s.apply(t)

	var out, errOut bytes.Buffer
	if code := run([]string{"update", "--check", "--format", "json"}, &out, &errOut); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "dns is down") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestUpdateDetectFailure(t *testing.T) {
	withVersion(t, "0.7.0")
	s := &updateStubs{installEr: errors.New("no executable")}
	s.apply(t)

	var out, errOut bytes.Buffer
	if code := run([]string{"update", "--check"}, &out, &errOut); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

// An unreadable local cask means Homebrew's receipt could not be confirmed, and
// a Caskroom-shaped path can be forged — so brew is told about, not run.
func TestUpdateUnconfirmedInstallIsNotUpgraded(t *testing.T) {
	withVersion(t, "0.7.0")
	s := &updateStubs{
		install: caskAt("0.7.0"), tapRemote: "0.8.0",
		tapLocErr: errors.New("no such file"),
	}
	s.apply(t)

	var out, errOut bytes.Buffer
	code := run([]string{"update", "--format", "json"}, &out, &errOut)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (nothing was upgraded)", code)
	}
	if s.upgrades != 0 {
		t.Errorf("ran %d upgrades without a confirmed receipt", s.upgrades)
	}
	if !strings.Contains(errOut.String(), update.BrewUpgradeCommand) {
		t.Errorf("stderr should name the command to run, got %q", errOut.String())
	}
}

// The release lookup is a bonus signal; losing it must not fail the command.
func TestUpdateReleaseLookupFailureIsTolerated(t *testing.T) {
	withVersion(t, "0.7.0")
	s := &updateStubs{
		install: caskAt("0.7.0"), tapRemote: "0.7.0", tapLocal: "0.7.0",
		releaseEr: errors.New("github is down"),
	}
	s.apply(t)

	var out, errOut bytes.Buffer
	if code := run([]string{"update", "--check", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}
}

func TestUpdateStyled(t *testing.T) {
	got := updateResult{
		CurrentVersion:  "0.7.0",
		LatestVersion:   "0.8.0",
		InstallMethod:   "homebrew_cask",
		UpdateAvailable: true,
		UpgradeCommand:  "brew upgrade --cask lk",
	}.Styled()

	for _, want := range []string{"0.7.0", "0.8.0", "brew upgrade --cask lk"} {
		if !strings.Contains(got, want) {
			t.Errorf("Styled() missing %q: %q", want, got)
		}
	}
}

func TestUpdateStyledUpToDate(t *testing.T) {
	got := updateResult{CurrentVersion: "0.7.0", LatestVersion: "0.7.0", InstallMethod: "homebrew_cask"}.Styled()
	if !strings.Contains(got, "up to date") {
		t.Errorf("Styled() = %q", got)
	}
}

func TestUpdateStyledWarning(t *testing.T) {
	got := updateResult{CurrentVersion: "0.7.0", LatestVersion: "0.7.0", Warning: "tap is behind"}.Styled()
	if !strings.Contains(got, "tap is behind") {
		t.Errorf("Styled() = %q", got)
	}
}

func TestUpdateRejectsArgs(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"update", "nonsense"}, &out, &errOut); code == 0 {
		t.Fatal("expected a non-zero exit for an unexpected argument")
	}
}
