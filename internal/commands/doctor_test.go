package commands

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func upServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/up" {
			w.WriteHeader(status)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func baseInput(t *testing.T, baseURL string) doctorInput {
	t.Helper()
	return doctorInput{
		version:    "test",
		goVersion:  "go1.26",
		os:         "darwin",
		arch:       "arm64",
		configPath: filepath.Join(t.TempDir(), "config.yml"),
		configDir:  t.TempDir(),
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 2 * time.Second},
		// Unset by default so no test reaches the network; the update check
		// then reports the same skip a real failed lookup would.
		latestVersion: func(context.Context) (string, error) {
			return "", errors.New("not wired in this test")
		},
	}
}

func findCheck(r *Result, name string) Check {
	for _, c := range r.Checks {
		if c.Name == name {
			return c
		}
	}
	return Check{}
}

func TestRunDoctorChecksAllPass(t *testing.T) {
	srv := upServer(t, http.StatusOK)
	r := runDoctorChecks(context.Background(), baseInput(t, srv.URL))

	if r.Failed != 0 || r.Warned != 0 {
		t.Fatalf("expected all pass, got %+v", r)
	}
	if r.Passed != 5 {
		t.Errorf("Passed = %d, want 5", r.Passed)
	}
	if got := findCheck(r, "API Reachability").Status; got != StatusPass {
		t.Errorf("reachability = %q", got)
	}
}

func TestRunDoctorReachabilityWarn(t *testing.T) {
	srv := upServer(t, http.StatusInternalServerError)
	r := runDoctorChecks(context.Background(), baseInput(t, srv.URL))
	if got := findCheck(r, "API Reachability").Status; got != StatusWarn {
		t.Errorf("reachability = %q, want warn", got)
	}
}

func TestRunDoctorReachabilityFail(t *testing.T) {
	srv := upServer(t, http.StatusOK)
	url := srv.URL
	srv.Close()

	in := baseInput(t, url)
	r := runDoctorChecks(context.Background(), in)
	if got := findCheck(r, "API Reachability").Status; got != StatusFail {
		t.Errorf("reachability = %q, want fail", got)
	}
	if r.Failed == 0 {
		t.Error("expected at least one failure")
	}
}

func TestCheckConfigBranches(t *testing.T) {
	if c := checkConfig("/p", errors.New("boom"), ""); c.Status != StatusFail {
		t.Errorf("loadErr → %q", c.Status)
	}
	if c := checkConfig("/p", nil, ""); c.Status != StatusWarn {
		t.Errorf("empty baseURL → %q", c.Status)
	}
	if c := checkConfig("/p", nil, "http://x"); c.Status != StatusPass {
		t.Errorf("ok → %q", c.Status)
	}
}

func TestCheckFilesystem(t *testing.T) {
	if c := checkFilesystem(t.TempDir()); c.Status != StatusPass {
		t.Errorf("writable dir → %q (%s)", c.Status, c.Hint)
	}
	// A path under a regular file cannot be created.
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if c := checkFilesystem(filepath.Join(file, "sub")); c.Status != StatusFail {
		t.Errorf("unwritable dir → %q", c.Status)
	}
}

func TestStatusIconDefault(t *testing.T) {
	if got := statusIcon("unknown"); got != "✗" {
		t.Errorf("statusIcon(unknown) = %q", got)
	}
}

func TestCheckReachabilityEmptyBaseURL(t *testing.T) {
	c := checkReachability(context.Background(), http.DefaultClient, "")
	if c.Status != StatusFail {
		t.Errorf("empty baseURL → %q", c.Status)
	}
}

func TestCheckReachabilityBuildError(t *testing.T) {
	c := checkReachability(context.Background(), http.DefaultClient, "http://bad\nhost")
	if c.Status != StatusFail {
		t.Errorf("bad URL → %q", c.Status)
	}
}

func TestSummary(t *testing.T) {
	pass := &Result{Passed: 3}
	if got := pass.Summary(); got != "All 3 checks passed" {
		t.Errorf("Summary = %q", got)
	}
	mixed := &Result{Passed: 1, Warned: 1, Failed: 1}
	if got := mixed.Summary(); got != "1 passed, 1 warned, 1 failed" {
		t.Errorf("Summary = %q", got)
	}
}

func TestStyledAndIcons(t *testing.T) {
	r := &Result{}
	r.add(Check{Name: "A", Status: StatusPass, Message: "ok"})
	r.add(Check{Name: "B", Status: StatusWarn, Message: "meh", Hint: "fix it"})
	r.add(Check{Name: "C", Status: StatusFail, Message: "bad"})

	out := r.Styled()
	for _, want := range []string{"✓ A", "! B", "✗ C", "↳ fix it"} {
		if !strings.Contains(out, want) {
			t.Errorf("Styled() missing %q in %q", want, out)
		}
	}
}

func TestDoctorCommandExitZero(t *testing.T) {
	srv := upServer(t, http.StatusOK)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LK_API_URL", srv.URL)

	var out, errOut bytes.Buffer
	code := run([]string{"doctor", "--format", "json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q, out = %q", code, errOut.String(), out.String())
	}
	if !strings.Contains(out.String(), `"API Reachability"`) {
		t.Errorf("output = %q", out.String())
	}
}

func TestDoctorCommandExitNonZeroOnFailure(t *testing.T) {
	srv := upServer(t, http.StatusOK)
	url := srv.URL
	srv.Close()

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LK_API_URL", url)

	var out, errOut bytes.Buffer
	code := run([]string{"doctor", "--format", "json"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

// --- Update check ---

func updateInput(t *testing.T, version string, latest func(context.Context) (string, error)) doctorInput {
	t.Helper()
	in := baseInput(t, upServer(t, http.StatusOK).URL)
	in.version = version
	if latest != nil {
		in.latestVersion = latest
	}
	return in
}

func constVersion(v string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return v, nil }
}

func TestDoctorUpdateUpToDate(t *testing.T) {
	r := runDoctorChecks(context.Background(), updateInput(t, "0.7.0", constVersion("0.7.0")))

	c := findCheck(r, "Update")
	if c.Status != StatusPass {
		t.Errorf("Update = %q (%s), want pass", c.Status, c.Message)
	}
}

// Being a version behind is not a broken install: doctor exits non-zero only
// on failures, so this must warn rather than fail.
func TestDoctorUpdateAvailableWarns(t *testing.T) {
	r := runDoctorChecks(context.Background(), updateInput(t, "0.7.0", constVersion("0.8.0")))

	c := findCheck(r, "Update")
	if c.Status != StatusWarn {
		t.Fatalf("Update = %q (%s), want warn", c.Status, c.Message)
	}
	if !strings.Contains(c.Message, "0.8.0") {
		t.Errorf("message = %q", c.Message)
	}
	if !strings.Contains(c.Hint, "lk update") {
		t.Errorf("hint = %q", c.Hint)
	}
	if r.Failed != 0 {
		t.Errorf("Failed = %d: an outdated CLI must not make doctor exit 1", r.Failed)
	}
}

func TestDoctorUpdateSkips(t *testing.T) {
	cases := []struct {
		name    string
		version string
		latest  func(context.Context) (string, error)
	}{
		{"development build", "dev", constVersion("9.9.9")},
		{"lookup failed", "0.7.0", func(context.Context) (string, error) {
			return "", errors.New("dns is down")
		}},
		// Newer is false for anything it cannot order, so a garbled version
		// from the tap must skip rather than read as "up to date".
		{"published version unorderable", "0.7.0", constVersion("not-a-version")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := runDoctorChecks(context.Background(), updateInput(t, c.version, c.latest))

			got := findCheck(r, "Update")
			if got.Status != StatusSkip {
				t.Errorf("Update = %q (%s), want skip", got.Status, got.Message)
			}
			if r.Failed != 0 || r.Warned != 0 {
				t.Errorf("a skipped update check must not fail or warn: %+v", r)
			}
		})
	}
}

// The whole command: an outdated CLI still exits 0.
func TestDoctorExitsZeroWhenOutdated(t *testing.T) {
	srv := upServer(t, http.StatusOK)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	withVersion(t, "0.7.0")

	prev := fetchTapRemote
	fetchTapRemote = func(context.Context, *http.Client) (string, error) { return "9.9.9", nil }
	t.Cleanup(func() { fetchTapRemote = prev })

	var out, errOut bytes.Buffer
	if code := run([]string{"doctor", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"Update"`) {
		t.Errorf("output has no Update check: %q", out.String())
	}
}
