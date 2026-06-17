package commands

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/linkanalabs/cli/internal/client"
)

func strptr(s string) *string { return &s }

func TestCheckAuthSkipNoToken(t *testing.T) {
	c := checkAuth(context.Background(), authCheckInput{reachable: true, hasToken: false})
	if c.Status != StatusSkip {
		t.Errorf("no token → %q, want skip", c.Status)
	}
}

func TestCheckAuthSkipUnreachable(t *testing.T) {
	c := checkAuth(context.Background(), authCheckInput{reachable: false, hasToken: true})
	if c.Status != StatusSkip {
		t.Errorf("unreachable → %q, want skip", c.Status)
	}
}

func TestCheckAuthPass(t *testing.T) {
	in := authCheckInput{
		reachable: true,
		hasToken:  true,
		identity: func(context.Context) (*client.Identity, error) {
			return &client.Identity{Email: "a@b.com", BuyerID: strptr("b_1")}, nil
		},
	}
	c := checkAuth(context.Background(), in)
	if c.Status != StatusPass {
		t.Errorf("valid token → %q, want pass", c.Status)
	}
	if c.Message == "" || c.Message == "a@b.com" && false {
		t.Errorf("message = %q", c.Message)
	}
}

func TestCheckAuthFailUnauthorized(t *testing.T) {
	in := authCheckInput{
		reachable: true,
		hasToken:  true,
		identity: func(context.Context) (*client.Identity, error) {
			return nil, client.ErrUnauthorized
		},
	}
	c := checkAuth(context.Background(), in)
	if c.Status != StatusFail {
		t.Errorf("401 → %q, want fail", c.Status)
	}
	if c.Hint == "" {
		t.Error("expected a re-login hint")
	}
}

func TestCheckAuthFailOther(t *testing.T) {
	in := authCheckInput{
		reachable: true,
		hasToken:  true,
		identity: func(context.Context) (*client.Identity, error) {
			return nil, errors.New("boom")
		},
	}
	c := checkAuth(context.Background(), in)
	if c.Status != StatusFail {
		t.Errorf("error → %q, want fail", c.Status)
	}
}

func TestRunDoctorChecksAuthSkippedByDefault(t *testing.T) {
	srv := upServer(t, 200)
	in := baseInput(t, srv.URL)
	r := runDoctorChecks(context.Background(), in)
	if got := findCheck(r, "Authentication").Status; got != StatusSkip {
		t.Errorf("auth check = %q, want skip (no token)", got)
	}
}

func TestRunDoctorChecksAuthPass(t *testing.T) {
	srv := upServer(t, 200)
	in := baseInput(t, srv.URL)
	in.hasToken = true
	in.identity = func(context.Context) (*client.Identity, error) {
		return &client.Identity{Email: "a@b.com"}, nil
	}
	r := runDoctorChecks(context.Background(), in)
	if got := findCheck(r, "Authentication").Status; got != StatusPass {
		t.Errorf("auth check = %q, want pass", got)
	}
}

func TestDoctorCommandAuthWired(t *testing.T) {
	authEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/up":
			w.WriteHeader(http.StatusOK)
		case "/my/identity.json":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"u_1","email":"doc@b.com","name":"Doc","role":"admin","buyer_id":null,"is_staff":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	code := run([]string{"doctor", "--format", "json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "doc@b.com") {
		t.Errorf("doctor output should include identity email: %q", out.String())
	}
}

func TestRunDoctorChecksAuthSkipCascade(t *testing.T) {
	// Reachability fails (closed server) → auth must skip.
	srv := upServer(t, 200)
	url := srv.URL
	srv.Close()
	in := baseInput(t, url)
	in.hasToken = true
	in.identity = func(context.Context) (*client.Identity, error) {
		t.Fatal("identity must not be called when unreachable")
		return nil, nil
	}
	r := runDoctorChecks(context.Background(), in)
	if got := findCheck(r, "Authentication").Status; got != StatusSkip {
		t.Errorf("auth check = %q, want skip on unreachable", got)
	}
}
