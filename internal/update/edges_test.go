package update

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestTagFromLocation(t *testing.T) {
	cases := []struct {
		loc  string
		want string
	}{
		{"https://github.com/linkanalabs/cli/releases/tag/v0.8.1", "v0.8.1"},
		{"/releases/tag/v1.0.0/", "v1.0.0"},
		{"/releases/tag/v1.0.0?utm=x", "v1.0.0"},
		{"/releases/tag/v1.0.0#notes", "v1.0.0"},
		{"https://github.com/linkanalabs/cli/releases", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := tagFromLocation(c.loc); got != c.want {
			t.Errorf("tagFromLocation(%q) = %q, want %q", c.loc, got, c.want)
		}
	}
}

// Leading zeros make an identifier non-numeric under semver, and a version
// core with one is not a version at all.
func TestVersionEdgeCases(t *testing.T) {
	if Comparable("01.0.0") {
		t.Error("01.0.0 was accepted as a version")
	}
	if Comparable("1.0.0-rc..1") {
		t.Error("an empty prerelease identifier was accepted")
	}

	// Refusing beats guessing: a version this package cannot read must not be
	// ordered, or garbage from the tap would be announced as an update.
	for _, v := range []string{
		"1.0.0-rc!1",  // illegal character in a prerelease identifier
		"1.0.0-01",    // numeric prerelease identifier with a leading zero
		"1.0.0+meta!", // illegal character in build metadata
		"1.0.0+",      // empty build metadata
		"1.0.0-rc.1+a..b",
	} {
		if Comparable(v) {
			t.Errorf("%q was accepted as a version", v)
		}
	}

	// Semver puts no ceiling on a numeric component, so a value far past what
	// an int holds must still order — and order correctly.
	huge := "1.0.99999999999999999999"      // 20 digits, beyond int64
	bigger := "1.0.99999999999999999999999" // 25 digits
	for _, v := range []string{huge, bigger} {
		if !Comparable(v) {
			t.Errorf("%q was refused; semver bounds no numeric component", v)
		}
	}
	if !Newer(bigger, huge) {
		t.Errorf("%q did not order above %q", bigger, huge)
	}
	if !Newer(huge, "1.0.9") {
		t.Errorf("%q did not order above 1.0.9", huge)
	}
	if Newer("1.0.9", huge) {
		t.Errorf("1.0.9 ordered above %q", huge)
	}
	// Same for a prerelease identifier, which must stay numeric rather than
	// falling back to lexical order.
	if !Newer("1.0.0-rc.99999999999999999999", "1.0.0-rc.9") {
		t.Error("a large numeric prerelease identifier compared lexically")
	}

	// ...while the legal shapes still are. Build metadata carries no
	// leading-zero rule (semver §10), unlike a prerelease identifier (§9), so
	// 1.0.0+001 must stay orderable.
	for _, v := range []string{"1.0.0-rc.1", "1.0.0-alpha-2", "1.0.0+build.5", "1.0.0-rc.1+build.5", "1.0.0+001", "1.0.0-rc.1+007"} {
		if !Comparable(v) {
			t.Errorf("%q was refused", v)
		}
	}
}

// Only Homebrew's actual layout counts. A binary sitting in any directory that
// happens to be called Caskroom must not be able to authorise brew replacing
// something — nor a cask that is not ours.
func TestDetectRequiresTheCaskLayout(t *testing.T) {
	if got := caskDir(string(filepath.Separator) + "Caskroom"); got != "" {
		t.Errorf("caskDir = %q, want empty", got)
	}

	cases := []struct {
		name string
		path []string
	}{
		{"caskroom with nothing under it", []string{"Caskroom"}},
		{"binary dropped straight into a Caskroom", []string{"Caskroom", "lk"}},
		{"a different cask", []string{"Caskroom", "ripgrep", "14.1.0", "lk"}},
		{"a directory that merely shares the name", []string{"Downloads", "Caskroom", "lk"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := string(filepath.Separator) + filepath.Join(c.path...)
			stubExecutable(t, p, nil)

			in, err := Detect()
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			if in.Homebrew() {
				t.Errorf("%q was treated as brew-managed", p)
			}
		})
	}
}

// The real layout still is a cask, at any depth below Caskroom/lk.
func TestDetectAcceptsTheRealCaskLayout(t *testing.T) {
	stubExecutable(t, filepath.Join(string(filepath.Separator)+"opt", "homebrew", "Caskroom", "lk", "0.7.0", "lk"), nil)

	in, err := Detect()
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if !in.Homebrew() {
		t.Error("the real Caskroom layout was not recognised")
	}
}

func TestReleaseRejectsUnusableURL(t *testing.T) {
	stubReleasesURL(t, "https://example.com/\x7f")

	if _, err := Release(context.Background(), http.DefaultClient); err == nil {
		t.Fatal("expected an error for a URL that cannot be turned into a request")
	}
}

func TestTapRemoteRejectsUnusableURL(t *testing.T) {
	stubTapURL(t, "https://example.com/\x7f")

	if _, err := TapRemote(context.Background(), http.DefaultClient); err == nil {
		t.Fatal("expected an error for a URL that cannot be turned into a request")
	}
}

// brew present but not runnable: the failure has to surface at start time.
func TestSpawnUpgradeStartFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits")
	}
	notExecutable := filepath.Join(t.TempDir(), "brew")
	if err := os.WriteFile(notExecutable, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubBrew(t, notExecutable, nil)

	if _, err := SpawnUpgrade(filepath.Join(t.TempDir(), "upgrade.log")); err == nil {
		t.Fatal("expected an error when brew cannot be executed")
	}
}
