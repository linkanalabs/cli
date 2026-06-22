package commands

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/linkanalabs/cli/internal/auth"
	"github.com/linkanalabs/cli/internal/output"
)

// impersonateServer mocks the backend: GET /my/identity.json (impersonator),
// POST /impersonation.json (mint), DELETE /impersonation.json (revoke).
func impersonateServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/my/identity.json":
			_, _ = w.Write([]byte(`{"id":"staff1","email":"staff@linkana.com","name":"Staff","role":"admin","buyer_id":"admin","is_staff":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/impersonation.json":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"lkn_imp_tok","identity":{"user_id":"u1","email":"suporte@linkana.com","buyer_id":"b1"},"expires_at":"2026-06-23T14:00:00Z"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/impersonation.json":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestImpersonateStartStoresContext(t *testing.T) {
	authEnv(t)
	srv := impersonateServer(t)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_original")

	var out, errOut strings.Builder
	code := run([]string{"impersonate", "suporte@linkana.com"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "suporte@linkana.com") || !strings.Contains(out.String(), "b1") {
		t.Errorf("stdout = %q", out.String())
	}
	imp, err := auth.LoadImpersonation(srv.URL)
	if err != nil || imp == nil {
		t.Fatalf("context not stored: imp=%v err=%v", imp, err)
	}
	if imp.Token != "lkn_imp_tok" || imp.ImpersonatorEmail != "staff@linkana.com" {
		t.Errorf("imp = %+v", *imp)
	}
}

func TestImpersonateStartNoToken(t *testing.T) {
	authEnv(t)
	srv := impersonateServer(t)
	t.Setenv("LK_API_URL", srv.URL)
	// no LK_TOKEN

	var out, errOut strings.Builder
	code := run([]string{"impersonate", "suporte@linkana.com"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "lk auth login") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestImpersonateStatusActive(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_API_URL", "http://localhost:3000")
	_ = auth.SaveImpersonation("http://localhost:3000", auth.Impersonation{
		Token: "lkn_imp", TargetEmail: "s@linkana.com", BuyerID: "b1",
		ImpersonatorEmail: "staff@linkana.com", ExpiresAt: time.Now().Add(time.Hour),
	})
	var out, errOut strings.Builder
	if code := run([]string{"impersonate", "status"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "s@linkana.com") {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestImpersonateStatusNone(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_API_URL", "http://localhost:3000")
	var out, errOut strings.Builder
	if code := run([]string{"impersonate", "status"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "nenhuma") {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestImpersonateStopClearsContext(t *testing.T) {
	authEnv(t)
	srv := impersonateServer(t)
	t.Setenv("LK_API_URL", srv.URL)
	_ = auth.SaveImpersonation(srv.URL, auth.Impersonation{
		Token: "lkn_imp", TargetEmail: "s@linkana.com", BuyerID: "b1",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	var out, errOut strings.Builder
	if code := run([]string{"impersonate", "stop"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if imp, _ := auth.LoadImpersonation(srv.URL); imp != nil {
		t.Errorf("context still present: %+v", *imp)
	}
}

func TestImpersonateStopWhenNone(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_API_URL", "http://localhost:3000")
	var out, errOut strings.Builder
	if code := run([]string{"impersonate", "stop"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String()+errOut.String(), "nenhuma") {
		t.Errorf("expected 'nenhuma' notice, got out=%q err=%q", out.String(), errOut.String())
	}
}

// --- extra coverage tests ---

func TestImpersonateStartConfigError(t *testing.T) {
	badConfigEnv(t)
	var out, errOut strings.Builder
	if code := run([]string{"impersonate", "suporte@linkana.com"}, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestImpersonateStartAuthLoadError(t *testing.T) {
	authEnv(t)
	swapAuthLoad(t, func(string) (string, auth.Source, error) {
		return "", auth.SourceNone, errors.New("keychain locked")
	})
	var out, errOut strings.Builder
	if code := run([]string{"impersonate", "suporte@linkana.com"}, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestImpersonateStartUnauthorized(t *testing.T) {
	authEnv(t)
	// Server always returns 401 for StartImpersonation.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_bad_tok")

	var out, errOut strings.Builder
	if code := run([]string{"impersonate", "suporte@linkana.com"}, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "lk auth login") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestImpersonateStopConfigError(t *testing.T) {
	badConfigEnv(t)
	var out, errOut strings.Builder
	if code := run([]string{"impersonate", "stop"}, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestImpersonateStopBestEffortFailure(t *testing.T) {
	authEnv(t)
	// Server returns 500 for DELETE (non-401 failure → warning, not hard error).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == "/impersonation.json":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LK_API_URL", srv.URL)
	_ = auth.SaveImpersonation(srv.URL, auth.Impersonation{
		Token: "lkn_imp", TargetEmail: "s@linkana.com", BuyerID: "b1",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	var out, errOut strings.Builder
	// Should succeed (best-effort) even with remote revoke failure.
	if code := run([]string{"impersonate", "stop"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	// Warning printed to stderr.
	if !strings.Contains(errOut.String(), "aviso") {
		t.Errorf("expected warning in stderr, got %q", errOut.String())
	}
	// Context cleared.
	if imp, _ := auth.LoadImpersonation(srv.URL); imp != nil {
		t.Errorf("context still present: %+v", *imp)
	}
}

func TestImpersonateStatusConfigError(t *testing.T) {
	badConfigEnv(t)
	var out, errOut strings.Builder
	if code := run([]string{"impersonate", "status"}, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestImpersonateStatusExpired(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_API_URL", "http://localhost:3000")
	past := time.Now().Add(-time.Hour)
	_ = auth.SaveImpersonation("http://localhost:3000", auth.Impersonation{
		Token: "lkn_imp", TargetEmail: "s@linkana.com", BuyerID: "b1",
		ExpiresAt: past,
	})
	// Fix timeNow so Expired() returns true.
	swapTimeNow(t, func() time.Time { return time.Now() })

	var out, errOut strings.Builder
	if code := run([]string{"impersonate", "status"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "EXPIRADA") {
		t.Errorf("expected EXPIRADA notice, got %q", combined)
	}
}

func TestImpersonateStatusJSON(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_API_URL", "http://localhost:3000")
	_ = auth.SaveImpersonation("http://localhost:3000", auth.Impersonation{
		Token: "lkn_imp", TargetEmail: "s@linkana.com", BuyerID: "b1",
		ImpersonatorEmail: "staff@linkana.com", ExpiresAt: time.Now().Add(time.Hour),
	})
	var out, errOut strings.Builder
	if code := run([]string{"impersonate", "status", "--format", output.FormatJSON}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	// Token must never appear in output.
	if strings.Contains(out.String(), "lkn_imp") {
		t.Errorf("token leaked in JSON output: %q", out.String())
	}
	if !strings.Contains(out.String(), "s@linkana.com") {
		t.Errorf("expected target_email in JSON, got %q", out.String())
	}
}

func TestImpersonateNoArgs(t *testing.T) {
	authEnv(t)
	var out, errOut strings.Builder
	// No subcommand and no args → prints help (exit 0).
	if code := run([]string{"impersonate"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
}
