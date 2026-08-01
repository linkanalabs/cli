package commands

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/state"
	"github.com/linkanalabs/cli/internal/update"
)

// autoStubs replaces everything maybeAutoUpdate touches and records what it
// actually did.
type autoStubs struct {
	install   *update.Install
	installEr error
	tapRemote string
	tapRemErr error
	tapLocal  string
	loaded    *state.State
	loadEr    error
	saveEr    error
	spawnEr   error
	logPathEr error
	now       time.Time

	fetches int
	spawns  int
	saved   []state.State
}

func (s *autoStubs) apply(t *testing.T) {
	t.Helper()

	// A real CI run sets these; clear them or the guarded tests never reach
	// the code they are meant to cover.
	for _, k := range ciEnvVars {
		t.Setenv(k, "")
	}
	t.Setenv(update.EnvNoAutoUpdate, "")

	if s.loaded == nil {
		s.loaded = &state.State{}
	}
	if s.now.IsZero() {
		s.now = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	}

	swap(t, &detectInstall, func() (*update.Install, error) { return s.install, s.installEr })
	swap(t, &fetchTapRemote, func(context.Context, *http.Client) (string, error) {
		s.fetches++
		return s.tapRemote, s.tapRemErr
	})
	swap(t, &readTapLocal, func(string) (string, error) { return s.tapLocal, nil })
	swap(t, &loadState, func() (*state.State, error) { return s.loaded, s.loadEr })
	swap(t, &saveState, func(st *state.State) error {
		s.saved = append(s.saved, *st)
		return s.saveEr
	})
	swap(t, &spawnUpgrade, func(string) (*exec.Cmd, error) {
		s.spawns++
		return &exec.Cmd{}, s.spawnEr
	})
	swap(t, &upgradeLogPath, func() (string, error) {
		if s.logPathEr != nil {
			return "", s.logPathEr
		}
		return "/tmp/lk/upgrade.log", nil
	})
	swapTimeNow(t, func() time.Time { return s.now })
}

// staleCheck is a last check old enough for a new one to be due.
func staleCheck(now time.Time) *state.State {
	return &state.State{LastCheckAt: now.Add(-48 * time.Hour)}
}

func upgradableStubs() *autoStubs {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	return &autoStubs{
		install:   caskAt("0.7.0"),
		tapRemote: "0.8.0",
		tapLocal:  "0.8.0",
		loaded:    staleCheck(now),
		now:       now,
	}
}

// The happy path: a day has passed, brew can install it, so lk starts the
// upgrade detached and says so on stderr.
func TestAutoUpdateUpgradesInBackground(t *testing.T) {
	withVersion(t, "0.7.0")
	s := upgradableStubs()
	s.apply(t)

	var errOut bytes.Buffer
	maybeAutoUpdate(&errOut, nil)

	if s.spawns != 1 {
		t.Fatalf("spawned %d upgrades, want 1", s.spawns)
	}
	notice := errOut.String()
	for _, want := range []string{"0.7.0", "0.8.0", "next run"} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice missing %q: %q", want, notice)
		}
	}
}

// stdout is the contract; the notice must never touch it.
func TestAutoUpdateNoticeGoesToStderrOnly(t *testing.T) {
	withVersion(t, "0.7.0")
	s := upgradableStubs()
	s.apply(t)

	var errOut bytes.Buffer
	maybeAutoUpdate(&errOut, nil)

	if errOut.Len() == 0 {
		t.Fatal("expected a notice on the writer lk was given for diagnostics")
	}
}

// The cadence promise: at most one network check per day, whatever happens.
func TestAutoUpdateThrottledWithinADay(t *testing.T) {
	withVersion(t, "0.7.0")
	s := upgradableStubs()
	s.loaded = &state.State{LastCheckAt: s.now.Add(-2 * time.Hour)}
	s.apply(t)

	var errOut bytes.Buffer
	maybeAutoUpdate(&errOut, nil)

	if s.fetches != 0 {
		t.Errorf("made %d network checks inside the 24h window, want 0", s.fetches)
	}
	if s.spawns != 0 {
		t.Errorf("spawned %d upgrades inside the 24h window", s.spawns)
	}
	if errOut.Len() != 0 {
		t.Errorf("wrote %q while throttled", errOut.String())
	}
}

// The day's budget is claimed before the request, so a network outage cannot
// turn into one attempt per command.
func TestAutoUpdateStampsBeforeFetching(t *testing.T) {
	withVersion(t, "0.7.0")
	s := upgradableStubs()
	s.tapRemErr = errors.New("network down")
	s.apply(t)

	maybeAutoUpdate(&bytes.Buffer{}, nil)

	if len(s.saved) == 0 {
		t.Fatal("the check timestamp was not persisted")
	}
	if !s.saved[0].LastCheckAt.Equal(s.now) {
		t.Errorf("LastCheckAt = %v, want %v", s.saved[0].LastCheckAt, s.now)
	}
	if s.spawns != 0 {
		t.Errorf("spawned an upgrade despite the failed lookup")
	}
}

// A failed state write means the throttle cannot be promised, so nothing else
// should happen.
func TestAutoUpdateStopsWhenStateCannotBeSaved(t *testing.T) {
	withVersion(t, "0.7.0")
	s := upgradableStubs()
	s.saveEr = errors.New("read-only fs")
	s.apply(t)

	maybeAutoUpdate(&bytes.Buffer{}, nil)

	if s.fetches != 0 {
		t.Errorf("made %d network checks without a usable throttle", s.fetches)
	}
}

// A stamp in the future — a clock correction, or an edited state file — must
// read as due. Throttling on it would disable updates until the clock caught
// up, possibly for years.
func TestAutoUpdateFutureStampIsDue(t *testing.T) {
	withVersion(t, "0.7.0")
	s := upgradableStubs()
	s.loaded = &state.State{LastCheckAt: s.now.Add(72 * time.Hour)}
	s.apply(t)

	var errOut bytes.Buffer
	maybeAutoUpdate(&errOut, nil)

	if s.fetches != 1 {
		t.Errorf("made %d checks with a future stamp, want 1", s.fetches)
	}
	if s.spawns != 1 {
		t.Errorf("spawned %d upgrades, want 1", s.spawns)
	}
}

func TestAutoUpdateSilentWhenCurrent(t *testing.T) {
	withVersion(t, "0.8.0")
	s := upgradableStubs()
	s.apply(t)

	var errOut bytes.Buffer
	maybeAutoUpdate(&errOut, nil)

	if s.spawns != 0 {
		t.Errorf("spawned %d upgrades on a current install", s.spawns)
	}
	if errOut.Len() != 0 {
		t.Errorf("wrote %q on a current install", errOut.String())
	}
}

// Brew's metadata trails the tap: lk says what to run and never runs a global
// `brew update` itself.
func TestAutoUpdateStaleMetadataOnlyAdvises(t *testing.T) {
	withVersion(t, "0.7.0")
	s := upgradableStubs()
	s.tapLocal = "0.7.0"
	s.apply(t)

	var errOut bytes.Buffer
	maybeAutoUpdate(&errOut, nil)

	if s.spawns != 0 {
		t.Fatalf("spawned %d upgrades against stale metadata", s.spawns)
	}
	if !strings.Contains(errOut.String(), update.BrewRefreshCommand) {
		t.Errorf("notice should name the refresh command, got %q", errOut.String())
	}
}

// "Read flags have no side effects" is a rule of this CLI: `lk --help` and
// `lk --version` must not reach the network or start an upgrade.
func TestAutoUpdateSkipsInformationalRuns(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"help flag", []string{"--help"}},
		{"version flag", []string{"--version"}},
		{"help flag on a subcommand", []string{"doctor", "--help"}},
		{"help command", []string{"help", "doctor"}},
		// The version command must behave exactly like the --version flag.
		{"version command", []string{"version"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withVersion(t, "0.7.0")
			s := upgradableStubs()
			s.apply(t)

			root := newRootCmd()
			root.SetArgs(c.args)
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			executed, err := root.ExecuteC()
			if err != nil {
				t.Fatalf("running %v: %v", c.args, err)
			}

			var errOut bytes.Buffer
			maybeAutoUpdate(&errOut, executed)

			if s.fetches != 0 {
				t.Errorf("%v made %d network checks", c.args, s.fetches)
			}
			if s.spawns != 0 {
				t.Errorf("%v spawned %d upgrades", c.args, s.spawns)
			}
			if len(s.saved) != 0 {
				t.Errorf("%v wrote state %d times", c.args, len(s.saved))
			}
		})
	}
}

// A command that merely failed is different: gating on success would mean a
// broken install — the one most in need of a fix — never updates.
func TestAutoUpdateStillRunsAfterAFailedCommand(t *testing.T) {
	withVersion(t, "0.7.0")
	s := upgradableStubs()
	s.apply(t)

	root := newRootCmd()
	root.SetArgs([]string{"doctor", "--format", "nope"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	executed, err := root.ExecuteC()
	if err == nil {
		t.Fatal("expected the bad flag to fail")
	}

	maybeAutoUpdate(&bytes.Buffer{}, executed)

	if s.fetches != 1 {
		t.Errorf("made %d network checks after a failed command, want 1", s.fetches)
	}
}

// Guards that must stop everything before any disk or network access.
func TestAutoUpdateGuards(t *testing.T) {
	cases := []struct {
		name    string
		version string
		setup   func(t *testing.T, s *autoStubs)
		cmd     *cobra.Command
	}{
		{
			name:    "opted out",
			version: "0.7.0",
			setup:   func(t *testing.T, _ *autoStubs) { t.Setenv(update.EnvNoAutoUpdate, "1") },
		},
		{
			name:    "continuous integration",
			version: "0.7.0",
			setup:   func(t *testing.T, _ *autoStubs) { t.Setenv("GITHUB_ACTIONS", "true") },
		},
		{
			name:    "development build",
			version: "dev",
		},
		{
			name:    "not a homebrew install",
			version: "0.7.0",
			setup: func(_ *testing.T, s *autoStubs) {
				s.install = &update.Install{Method: update.MethodOther}
			},
		},
		{
			name:    "install cannot be determined",
			version: "0.7.0",
			setup: func(_ *testing.T, s *autoStubs) {
				s.installEr = errors.New("no executable")
			},
		},
		{
			name:    "state unreadable",
			version: "0.7.0",
			setup:   func(_ *testing.T, s *autoStubs) { s.loadEr = errors.New("corrupt") },
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withVersion(t, c.version)
			s := upgradableStubs()
			s.apply(t)
			if c.setup != nil {
				c.setup(t, s)
			}

			var errOut bytes.Buffer
			maybeAutoUpdate(&errOut, c.cmd)

			if s.fetches != 0 {
				t.Errorf("made %d network checks", s.fetches)
			}
			if s.spawns != 0 {
				t.Errorf("spawned %d upgrades", s.spawns)
			}
			if errOut.Len() != 0 {
				t.Errorf("wrote %q", errOut.String())
			}
		})
	}
}

// `lk update` already did this synchronously; the automatic path must stand
// down rather than start a second brew.
func TestAutoUpdateSkipsTheUpdateCommandItself(t *testing.T) {
	withVersion(t, "0.7.0")
	s := upgradableStubs()
	s.apply(t)

	root := newRootCmd()
	var updateCmd *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "update" {
			updateCmd = c
		}
	}
	if updateCmd == nil {
		t.Fatal("no update command on the root")
	}

	var errOut bytes.Buffer
	maybeAutoUpdate(&errOut, updateCmd)

	if s.fetches != 0 || s.spawns != 0 {
		t.Errorf("ran the automatic path during `lk update` (%d fetches, %d spawns)", s.fetches, s.spawns)
	}
}

// The opt-out is declared on the command, so the nested `lk settings ... update`
// — which shares a name but not the annotation — is unaffected.
func TestSkipsAutoUpdateIsDeclaredNotNameMatched(t *testing.T) {
	root := newRootCmd()

	var topLevel, nested *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "update" {
			topLevel = c
		}
		if c.Name() == "settings" {
			for _, g := range c.Commands() {
				for _, leaf := range g.Commands() {
					if leaf.Name() == "update" && nested == nil {
						nested = leaf
					}
				}
			}
		}
	}
	if topLevel == nil || nested == nil {
		t.Skip("command tree does not carry both an update command and a nested one")
	}

	if !skipsAutoUpdate(topLevel) {
		t.Errorf("%q did not opt out of the automatic check", topLevel.CommandPath())
	}
	if skipsAutoUpdate(nested) {
		t.Errorf("%q was wrongly opted out", nested.CommandPath())
	}
	if skipsAutoUpdate(nil) {
		t.Error("nil was treated as opted out")
	}
}

func TestAutoUpdateSpawnFailureIsReported(t *testing.T) {
	withVersion(t, "0.7.0")
	s := upgradableStubs()
	s.spawnEr = errors.New("brew is missing")
	s.apply(t)

	var errOut bytes.Buffer
	maybeAutoUpdate(&errOut, nil)

	if !strings.Contains(errOut.String(), "brew is missing") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestAutoUpdateStopsWhenLogPathUnavailable(t *testing.T) {
	withVersion(t, "0.7.0")
	s := upgradableStubs()
	s.logPathEr = errors.New("no home")
	s.apply(t)

	var errOut bytes.Buffer
	maybeAutoUpdate(&errOut, nil)

	if s.spawns != 0 {
		t.Errorf("spawned an upgrade with nowhere to log it")
	}
}

// The daily budget is claimed once and is the only thing written: it is what
// bounds retries, so a crashed upgrade costs a day rather than looping.
func TestAutoUpdateWritesOnlyTheCheckStamp(t *testing.T) {
	withVersion(t, "0.7.0")
	s := upgradableStubs()
	s.apply(t)

	maybeAutoUpdate(&bytes.Buffer{}, nil)

	if len(s.saved) != 1 {
		t.Fatalf("wrote state %d times, want 1", len(s.saved))
	}
	if !s.saved[0].LastCheckAt.Equal(s.now) {
		t.Errorf("LastCheckAt = %v, want %v", s.saved[0].LastCheckAt, s.now)
	}
}
