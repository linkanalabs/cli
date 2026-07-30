package commands

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/linkanalabs/cli/internal/auth"
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
