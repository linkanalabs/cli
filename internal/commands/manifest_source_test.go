package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/linkanalabs/cli/internal/auth"
	"github.com/linkanalabs/cli/internal/manifest"
)

const backendOnlyManifest = `{"manifest_version":1,"generated_at":"2026-11-16T00:00:00Z","source":"linkanalabs/linkana@backend",
"endpoints":[{"command":["gadget","list"],"summary":"List the buyer's gadgets","method":"GET","path":"/srm/gadgets","path_params":[],"params":[],"response":"200"}]}`

// remoteBackend serves backendOnlyManifest on the manifest endpoint and an
// empty array on the gadget endpoint, recording which paths were asked.
func remoteBackend(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case manifest.RemotePath:
			_, _ = w.Write([]byte(backendOnlyManifest))
		case "/srm/gadgets.json":
			_, _ = w.Write([]byte(`[{"id":"g1"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &paths
}

// useRemoteManifest lets run() ask the test backend for its manifest.
func useRemoteManifest(t *testing.T, baseURL string) {
	t.Helper()
	t.Setenv(EnvNoRemoteManifest, "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LK_API_URL", baseURL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")
	t.Setenv("LK_NO_KEYRING", "1")
}

func TestBackendCommandShowsUpWithoutUpdatingLk(t *testing.T) {
	server, paths := remoteBackend(t)
	useRemoteManifest(t, server.URL)

	var out, errOut bytes.Buffer
	if code := run([]string{"gadget", "list", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}

	if !strings.Contains(out.String(), `"g1"`) {
		t.Errorf("stdout = %q", out.String())
	}
	if got := strings.Join(*paths, ","); got != manifest.RemotePath+",/srm/gadgets.json" {
		t.Errorf("paths = %s", got)
	}
}

func TestBackendCommandStillRunsFromTheCopyWhenTheBackendManifestFails(t *testing.T) {
	server, _ := remoteBackend(t)
	useRemoteManifest(t, server.URL)
	var out, errOut bytes.Buffer
	run([]string{"--help"}, &out, &errOut)

	timeNow = func() time.Time { return time.Now().Add(manifest.DefaultMaxAge + time.Minute) }
	t.Cleanup(func() { timeNow = time.Now })
	manifestAsked := false
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == manifest.RemotePath {
			manifestAsked = true
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	})

	out.Reset()
	if code := run([]string{"gadget", "list", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	if !manifestAsked {
		t.Errorf("the copy was older than the max age but the backend was not asked")
	}
}

func TestOfflineCommandsNeverAskTheBackend(t *testing.T) {
	server, paths := remoteBackend(t)
	useRemoteManifest(t, server.URL)

	var out, errOut bytes.Buffer
	for _, args := range [][]string{{"version"}, {"--version"}, {"config", "show"}, {"auth", "status"}} {
		run(args, &out, &errOut)
	}

	if len(*paths) != 0 {
		t.Errorf("backend asked: %v", *paths)
	}
}

func TestVersionReportsTheBackendCopy(t *testing.T) {
	server, _ := remoteBackend(t)
	useRemoteManifest(t, server.URL)
	var out, errOut bytes.Buffer
	run([]string{"--help"}, &out, &errOut)

	out.Reset()
	run([]string{"version", "--format", "json"}, &out, &errOut)

	var info versionInfo
	if err := json.Unmarshal(out.Bytes(), &info); err != nil {
		t.Fatalf("parsing %q: %v", out.String(), err)
	}
	if info.Manifest == nil || info.Manifest.Origin != manifest.OriginCache || info.Manifest.Source != "linkanalabs/linkana@backend" {
		t.Errorf("manifest = %+v", info.Manifest)
	}
}

func TestWantsFreshManifest(t *testing.T) {
	t.Setenv(EnvNoRemoteManifest, "")
	cases := map[string]bool{
		"":                        true,
		"--help":                  true,
		"supplier list":           true,
		"--format json cli x":     true,
		"version":                 false,
		"--version":               false,
		"auth login":              false,
		"--format json doctor":    false,
		"config set-url x":        false,
		"update":                  false,
		"completion bash":         false,
		"supplier list --version": false,
	}
	for line, want := range cases {
		if got := wantsFreshManifest(strings.Fields(line)); got != want {
			t.Errorf("wantsFreshManifest(%q) = %v, want %v", line, got, want)
		}
	}
	t.Setenv(EnvNoRemoteManifest, "1")
	if wantsFreshManifest([]string{"supplier", "list"}) {
		t.Errorf("LK_NO_REMOTE_MANIFEST did not turn the fetch off")
	}
}

func TestManifestTokenPrefersALiveImpersonation(t *testing.T) {
	useRemoteManifest(t, "https://example.test")
	swapAuthLoad(t, func(string) (string, auth.Source, error) { return "lkn_own", auth.SourceEnv, nil })

	if got := manifestToken("https://example.test"); got != "lkn_own" {
		t.Errorf("without impersonation: %q", got)
	}

	if err := auth.SaveImpersonation("https://example.test", auth.Impersonation{Token: "lkn_imp", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if got := manifestToken("https://example.test"); got != "lkn_imp" {
		t.Errorf("with impersonation: %q", got)
	}

	if err := auth.SaveImpersonation("https://example.test", auth.Impersonation{Token: "lkn_imp", ExpiresAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if got := manifestToken("https://example.test"); got != "" {
		t.Errorf("with expired impersonation: %q", got)
	}
}

func TestManifestTokenIsEmptyWhenTheStoreFails(t *testing.T) {
	useRemoteManifest(t, "https://example.test")
	swapAuthLoad(t, func(string) (string, auth.Source, error) { return "", "", errNoToken })

	if got := manifestToken("https://example.test"); got != "" {
		t.Errorf("token = %q", got)
	}
}
