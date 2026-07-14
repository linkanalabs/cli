package commands

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/linkanalabs/cli/internal/auth"
	"github.com/linkanalabs/cli/internal/client"
	"github.com/linkanalabs/cli/internal/config"
	"github.com/linkanalabs/cli/internal/mode"
)

// identityServer serves /my/identity.json with the given status and body.
func identityServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/my/identity.json" {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestWhoamiSuccess(t *testing.T) {
	authEnv(t)
	srv := identityServer(t, http.StatusOK, `{"id":"u_1","email":"a@b.com","name":"Ana","role":"admin","buyer_id":null,"is_staff":true}`)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	code := run([]string{"whoami", "--format", "json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"a@b.com"`) {
		t.Errorf("output = %q", out.String())
	}
}

func TestWhoamiStyled(t *testing.T) {
	authEnv(t)
	srv := identityServer(t, http.StatusOK, `{"id":"u_1","email":"a@b.com","name":"Ana","role":"admin","buyer_id":"b_2","is_staff":false}`)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	code := run([]string{"whoami", "--format", "styled"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	for _, want := range []string{"a@b.com", "Ana", "admin", "b_2"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("styled output missing %q: %q", want, out.String())
		}
	}
}

func TestWhoamiNoToken(t *testing.T) {
	authEnv(t)
	srv := identityServer(t, http.StatusOK, `{}`)
	t.Setenv("LK_API_URL", srv.URL)

	var out, errOut bytes.Buffer
	code := run([]string{"whoami"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "lk auth login") {
		t.Errorf("stderr should hint login: %q", errOut.String())
	}
}

func TestWhoamiUnauthorized(t *testing.T) {
	authEnv(t)
	srv := identityServer(t, http.StatusUnauthorized, ``)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_bad_token")

	var out, errOut bytes.Buffer
	code := run([]string{"whoami"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "lk auth login") {
		t.Errorf("stderr should hint re-login: %q", errOut.String())
	}
}

func TestWhoamiLoadError(t *testing.T) {
	authEnv(t)
	swapAuthLoad(t, func(string) (string, auth.Source, error) {
		return "", auth.SourceNone, errors.New("keychain locked")
	})
	var out, errOut bytes.Buffer
	if code := run([]string{"whoami"}, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestWhoamiServerError(t *testing.T) {
	authEnv(t)
	srv := identityServer(t, http.StatusInternalServerError, ``)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	code := run([]string{"whoami"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestAuthedClientInjectsMode(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LK_NO_KEYRING", "1")
	t.Setenv("LK_TOKEN", "lkn_x_y")
	t.Setenv("LK_API_URL", "http://localhost:3000")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := mode.Save(cfg.BaseURL, mode.Write); err != nil {
		t.Fatal(err)
	}
	var gotMode mode.Mode
	prev := newAPI
	newAPI = func(baseURL, token string, m mode.Mode) client.API {
		gotMode = m
		c := client.New(baseURL)
		c.Token = token
		c.Mode = m
		return c
	}
	defer func() { newAPI = prev }()
	if _, _, _, _, err := authedClient(); err != nil {
		t.Fatalf("authedClient: %v", err)
	}
	if gotMode != mode.Write {
		t.Errorf("mode = %q, want write", gotMode)
	}
}

func TestWhoamiShowsMode(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	authEnv(t)
	srv := identityServer(t, http.StatusOK, `{"id":"u_1","email":"a@b.com","name":"Ana","role":"admin","buyer_id":null,"is_staff":true}`)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	// Persist write mode for this origin so whoami reflects it.
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if err := mode.Save(cfg.BaseURL, mode.Write); err != nil {
		t.Fatalf("mode.Save: %v", err)
	}

	var out, errOut bytes.Buffer
	code := run([]string{"whoami", "--format", "json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"write"`) {
		t.Errorf("output missing write mode value: %q", out.String())
	}
}

func TestAuthedClientModeLoadError(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("LK_NO_KEYRING", "1")
	t.Setenv("LK_TOKEN", "lkn_x_y")
	t.Setenv("LK_API_URL", "http://localhost:3000")

	// Make modes.json a directory so mode.Load hits a read error (not a parse
	// error, which now fails safe to read) and authedClient propagates it.
	lkDir := configDir + "/lk"
	if err := os.MkdirAll(lkDir+"/modes.json", 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	_, _, _, _, err := authedClient()
	if err == nil {
		t.Fatal("authedClient: expected error from mode.Load, got nil")
	}
	if !strings.Contains(err.Error(), "loading mode") {
		t.Errorf("error should mention 'loading mode': %v", err)
	}
}
