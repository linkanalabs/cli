package commands

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/state"
	"github.com/linkanalabs/cli/internal/update"
)

const (
	// autoUpdateInterval is the whole cadence: lk touches the network for
	// version information at most this often, no matter how many commands run.
	autoUpdateInterval = 24 * time.Hour

	// autoUpdateTimeout keeps a slow network from holding the process open.
	autoUpdateTimeout = 3 * time.Second

	// annotationNoAutoUpdate marks a command that must not trigger the
	// automatic check. Declaring it on the command generalises: the guard never
	// has to keep a list of command names.
	annotationNoAutoUpdate = "lk:no-auto-update"
)

// ciEnvVars mark an automated environment. Upgrading a binary underneath a
// build would make it non-reproducible, so lk never does it there.
var ciEnvVars = []string{"CI", "GITHUB_ACTIONS", "BUILDKITE", "CIRCLECI", "GITLAB_CI", "JENKINS_URL", "TEAMCITY_VERSION", "TF_BUILD"}

var (
	loadState    = state.Load
	saveState    = func(s *state.State) error { return s.Save() }
	spawnUpgrade = update.SpawnUpgrade
)

// maybeAutoUpdate keeps the installation current without being asked, at most
// once a day.
//
// It is called from Execute — the process edge — and never from run(), which
// stays the pure testable core. Writing to ~/.local/state, reaching a CDN and
// forking a detached brew are process effects, not part of "execute the command
// tree", and keeping them out of run() means no test reaches them by accident.
//
// It runs *after* the command has produced its output and its exit code is
// settled. Running it earlier would only add latency: the process already in
// flight is the old binary either way, so a new version can only ever apply
// from the next invocation — which is what the notice says.
//
// Unlike gh and fizzy, this is not gated on stdout being a terminal. lk's
// primary user is an agent, so a TTY gate would disable the feature precisely
// where it was asked for. Isolation comes from the notice going to stderr and
// never to stdout, which stays the JSON contract.
func maybeAutoUpdate(stderr io.Writer, executed *cobra.Command) {
	// Every guard below is local and free. Nothing reads the disk until the
	// install is known to be one brew can act on, and nothing touches the
	// network until the once-a-day budget has been claimed.
	if os.Getenv(update.EnvNoAutoUpdate) != "" || isCI() || skipsAutoUpdate(executed) || informational(executed) {
		return
	}
	// A development build has no version to compare against.
	if !update.Comparable(version) {
		return
	}
	// Path arithmetic only — Detect reads no files.
	in, err := detectInstall()
	if err != nil || !in.Homebrew() {
		return
	}

	st, err := loadState()
	if err != nil {
		return
	}
	// A negative elapsed means the stamp is in the future — a clock correction,
	// or an edited state file. Treat that as due: the alternative is silently
	// disabling updates until the clock catches up, possibly for years.
	if elapsed := timeNow().Sub(st.LastCheckAt); elapsed >= 0 && elapsed < autoUpdateInterval {
		return
	}

	// Claim the day's budget *before* the network call, not after: a second lk
	// exiting afterwards reads the fresh stamp and stands down, and a network
	// outage cannot turn into a request on every single command. The cost is
	// that a transient failure waits for tomorrow, which is the right trade for
	// something nobody asked to run.
	//
	// Load-check-save is not atomic across processes, so two lk instances
	// exiting within the same few hundred microseconds can both claim it. That
	// is left alone deliberately: the window is tiny, the worst case is one
	// redundant fetch and a second `brew upgrade` that brew itself serialises on
	// its own lock, and an interprocess lock would trade that for stale-lock
	// recovery on every run.
	st.LastCheckAt = timeNow()
	if err := saveState(st); err != nil {
		return // cannot promise the throttle, so do not proceed
	}

	ctx, cancel := context.WithTimeout(context.Background(), autoUpdateTimeout)
	defer cancel()

	hc := &http.Client{Timeout: autoUpdateTimeout}
	tapRemote, err := fetchTapRemote(ctx, hc)
	if err != nil {
		return
	}

	d := update.Decide(update.Inputs{
		Install:   in,
		Current:   version,
		TapRemote: tapRemote,
		TapLocal:  tapLocalFor(in),
	})
	if !d.UpdateAvailable {
		return
	}

	if !d.CanSelfUpgrade {
		// Brew's metadata trails the tap, so an upgrade would be a no-op.
		// Refreshing every tap on the machine is the user's call.
		_, _ = fmt.Fprintf(stderr, "lk: %s is available; run: %s\n", d.Latest, d.Command)
		return
	}
	launchUpgrade(stderr, d.Latest)
}

// launchUpgrade starts brew in the background and says so. Nothing is recorded
// about the attempt: the daily budget above already bounds retries to one a
// day, and what brew actually did is in the log rather than guessed at from a
// flag on disk.
func launchUpgrade(stderr io.Writer, latest string) {
	logPath, err := upgradeLogPath()
	if err != nil {
		return
	}
	if _, err := spawnUpgrade(logPath); err != nil {
		_, _ = fmt.Fprintf(stderr, "lk: could not start the upgrade to %s: %v\n", latest, err)
		return
	}
	_, _ = fmt.Fprintf(stderr,
		"lk: upgrading %s → %s in the background; it applies from the next run (log: %s)\n",
		version, latest, logPath)
}

// informational reports whether this run only printed help or the version.
// "Read flags have no side effects" is a rule of this CLI, and `lk --help`
// quietly reaching the network and starting a background brew upgrade would
// break it — however throttled. Unlike a command that merely failed, these
// paths never execute a command body at all.
func informational(c *cobra.Command) bool {
	if c == nil {
		return false
	}
	// cobra's built-in `help` command, as in `lk help update`.
	if c.Name() == "help" {
		return true
	}
	for _, name := range []string{"help", "version"} {
		if f := c.Flags().Lookup(name); f != nil && f.Changed {
			return true
		}
	}
	return false
}

func isCI() bool {
	for _, k := range ciEnvVars {
		if os.Getenv(k) != "" {
			return true
		}
	}
	return false
}

// skipsAutoUpdate reports whether the command that ran opted out, itself or
// through an ancestor. `lk update` does the job explicitly and synchronously,
// so checking again behind its back would be redundant and could start a second
// brew.
//
// The command declares this rather than the guard matching names: comparing
// against "lk update" would break silently if the root were renamed, and would
// need a new special case for every future command that wants out.
func skipsAutoUpdate(c *cobra.Command) bool {
	for ; c != nil; c = c.Parent() {
		if c.Annotations[annotationNoAutoUpdate] != "" {
			return true
		}
	}
	return false
}
