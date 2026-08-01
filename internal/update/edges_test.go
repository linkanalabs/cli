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
}

// A path whose Caskroom segment has nothing above it yields no metadata
// directory, and detection must simply carry on without receipt data.
func TestCaskDirWithoutAParent(t *testing.T) {
	if got := caskDir(string(filepath.Separator) + "Caskroom"); got != "" {
		t.Errorf("caskDir = %q, want empty", got)
	}

	stubExecutable(t, string(filepath.Separator)+"Caskroom", nil)
	in, err := Detect()
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if in.Method != MethodHomebrewCask {
		t.Errorf("Method = %q", in.Method)
	}
	if in.CaskPath() != "" {
		t.Errorf("CaskPath() = %q, want empty", in.CaskPath())
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
