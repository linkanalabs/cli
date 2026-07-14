package commands

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/linkanalabs/cli/internal/auth"
	"github.com/linkanalabs/cli/internal/mode"
	"github.com/linkanalabs/cli/internal/output"
)

// enableWrite puts the origin in write mode: `impersonate` mints/revokes via
// POST/DELETE, which the read/write gate blocks in read mode.
func enableWrite(t *testing.T, baseURL string) {
	t.Helper()
	if err := mode.Save(baseURL, mode.Write); err != nil {
		t.Fatal(err)
	}
}

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
	enableWrite(t, srv.URL)

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

// TestImpersonateStatusNone verifies styled output when no context is active.
// (styled is explicit here because auto→json in non-TTY tests; the JSON case
// is covered by TestImpersonateStatusNoneJSON.)
func TestImpersonateStatusNone(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_API_URL", "http://localhost:3000")
	var out, errOut strings.Builder
	if code := run([]string{"impersonate", "status", "--format", "styled"}, &out, &errOut); code != 0 {
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

// TestImpersonateStopWhenNone verifies styled output when no context is active.
// (styled is explicit; JSON case is TestImpersonateStopJSONNoContext.)
func TestImpersonateStopWhenNone(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_API_URL", "http://localhost:3000")
	var out, errOut strings.Builder
	if code := run([]string{"impersonate", "stop", "--format", "styled"}, &out, &errOut); code != 0 {
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
	enableWrite(t, srv.URL)

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

// TestImpersonateStatusExpired uses --format styled to verify the EXPIRADA notice.
// (In non-TTY tests auto→json so styled must be explicit for the text assertion.)
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
	if code := run([]string{"impersonate", "status", "--format", "styled"}, &out, &errOut); code != 0 {
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

// TestImpersonateStartRevokesExistingContext asserts that when a previous
// impersonation context is active, starting a new one issues a best-effort
// DELETE for the old token before minting and storing the new one.
func TestImpersonateStartRevokesExistingContext(t *testing.T) {
	authEnv(t)

	var deleteCount int
	var deletedToken string

	// Two different impersonation tokens: old one (lkn_old_imp) is already stored;
	// the server mints lkn_new_imp on the new POST.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/my/identity.json":
			_, _ = w.Write([]byte(`{"id":"staff1","email":"staff@linkana.com","name":"Staff","role":"admin","buyer_id":"admin","is_staff":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/impersonation.json":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"lkn_new_imp","identity":{"user_id":"u2","email":"new@linkana.com","buyer_id":"b2"},"expires_at":"2026-06-23T14:00:00Z"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/impersonation.json":
			deleteCount++
			deletedToken = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_original")
	enableWrite(t, srv.URL)

	// Pre-store an existing impersonation.
	_ = auth.SaveImpersonation(srv.URL, auth.Impersonation{
		Token: "lkn_old_imp", TargetEmail: "old@linkana.com", BuyerID: "b_old",
		ImpersonatorEmail: "staff@linkana.com", ExpiresAt: time.Now().Add(time.Hour),
	})

	var out, errOut strings.Builder
	code := run([]string{"impersonate", "new@linkana.com"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q, stdout = %q", code, errOut.String(), out.String())
	}

	// A DELETE must have been issued for the OLD token.
	if deleteCount == 0 {
		t.Error("expected a DELETE /impersonation.json for the prior token, got none")
	}
	if deletedToken != "Bearer lkn_old_imp" {
		t.Errorf("DELETE used wrong token: got %q, want \"Bearer lkn_old_imp\"", deletedToken)
	}

	// The new impersonation must be stored.
	imp, err := auth.LoadImpersonation(srv.URL)
	if err != nil || imp == nil {
		t.Fatalf("new context not stored: imp=%v err=%v", imp, err)
	}
	if imp.Token != "lkn_new_imp" {
		t.Errorf("stored token = %q, want lkn_new_imp", imp.Token)
	}
	if imp.TargetEmail != "new@linkana.com" {
		t.Errorf("stored target = %q, want new@linkana.com", imp.TargetEmail)
	}
}

// TestImpersonateStartIdentityFailureWarns verifies that when POST /impersonation.json
// succeeds (201) but GET /my/identity.json returns 500, the command still exits 0,
// the impersonation context IS stored (token + target email correct), and a warning
// containing "impersonador" or "identity" is printed to STDERR.
func TestImpersonateStartIdentityFailureWarns(t *testing.T) {
	authEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/my/identity.json":
			w.WriteHeader(http.StatusInternalServerError)
		case r.Method == http.MethodPost && r.URL.Path == "/impersonation.json":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"lkn_imp_tok","identity":{"user_id":"u1","email":"suporte@linkana.com","buyer_id":"b1"},"expires_at":"2026-06-23T14:00:00Z"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_original")
	enableWrite(t, srv.URL)

	var out, errOut strings.Builder
	code := run([]string{"impersonate", "suporte@linkana.com"}, &out, &errOut)
	// Must exit 0 even though identity lookup failed.
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q, stdout = %q", code, errOut.String(), out.String())
	}
	// Impersonation context must be stored with correct token and target email.
	imp, err := auth.LoadImpersonation(srv.URL)
	if err != nil || imp == nil {
		t.Fatalf("context not stored: imp=%v err=%v", imp, err)
	}
	if imp.Token != "lkn_imp_tok" {
		t.Errorf("imp.Token = %q, want lkn_imp_tok", imp.Token)
	}
	if imp.TargetEmail != "suporte@linkana.com" {
		t.Errorf("imp.TargetEmail = %q, want suporte@linkana.com", imp.TargetEmail)
	}
	// ImpersonatorEmail must be empty (identity lookup failed).
	if imp.ImpersonatorEmail != "" {
		t.Errorf("imp.ImpersonatorEmail = %q, want empty", imp.ImpersonatorEmail)
	}
	// A warning must be printed to stderr mentioning the failure.
	stderrStr := errOut.String()
	if !strings.Contains(stderrStr, "impersonador") && !strings.Contains(stderrStr, "identity") {
		t.Errorf("expected warning about impersonador/identity in stderr, got %q", stderrStr)
	}
}

// --- Finding A: `impersonate status` no-context in JSON mode → null ---

// TestImpersonateStatusNoneJSON asserts that JSON output is `null` (parseable)
// when there is no active impersonation.
func TestImpersonateStatusNoneJSON(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_API_URL", "http://localhost:3000")
	var out, errOut strings.Builder
	if code := run([]string{"impersonate", "status", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	raw := strings.TrimSpace(out.String())
	// Must be parseable JSON that represents null.
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("stdout is not valid JSON: %v; got %q", err, raw)
	}
	if v != nil {
		t.Errorf("expected JSON null, got %v (%q)", v, raw)
	}
	// Token must never appear.
	if strings.Contains(raw, "lkn") {
		t.Errorf("token leaked in output: %q", raw)
	}
}

// TestImpersonateStatusActiveJSONNoToken asserts that with an active context,
// JSON output is a valid object with target_email but NO token field.
func TestImpersonateStatusActiveJSONNoToken(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_API_URL", "http://localhost:3000")
	_ = auth.SaveImpersonation("http://localhost:3000", auth.Impersonation{
		Token: "lkn_secret_tok", TargetEmail: "buyer@linkana.com", BuyerID: "b42",
		ImpersonatorEmail: "staff@linkana.com", ExpiresAt: time.Now().Add(time.Hour),
	})
	var out, errOut strings.Builder
	if code := run([]string{"impersonate", "status", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	raw := out.String()
	// Must be valid JSON object.
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &m); err != nil {
		t.Fatalf("stdout is not valid JSON: %v; got %q", err, raw)
	}
	// Must contain target_email.
	if m["target_email"] != "buyer@linkana.com" {
		t.Errorf("target_email = %v, got full map %v", m["target_email"], m)
	}
	// Token must never appear.
	if strings.Contains(raw, "lkn_secret_tok") {
		t.Errorf("token leaked in JSON: %q", raw)
	}
}

// --- Finding B: `impersonate stop` JSON output ---

// TestImpersonateStopJSONWithContext asserts that stopping with an active context
// emits {stopped:true, target_email:"..."} in JSON mode, with no token.
func TestImpersonateStopJSONWithContext(t *testing.T) {
	authEnv(t)
	srv := impersonateServer(t)
	t.Setenv("LK_API_URL", srv.URL)
	_ = auth.SaveImpersonation(srv.URL, auth.Impersonation{
		Token: "lkn_stop_tok", TargetEmail: "buyer@linkana.com", BuyerID: "b1",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	var out, errOut strings.Builder
	if code := run([]string{"impersonate", "stop", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	raw := out.String()
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &m); err != nil {
		t.Fatalf("stdout is not valid JSON: %v; got %q", err, raw)
	}
	if m["stopped"] != true {
		t.Errorf("stopped = %v, want true", m["stopped"])
	}
	if m["target_email"] != "buyer@linkana.com" {
		t.Errorf("target_email = %v, want buyer@linkana.com", m["target_email"])
	}
	// Token must never appear.
	if strings.Contains(raw, "lkn_stop_tok") {
		t.Errorf("token leaked in JSON: %q", raw)
	}
}

// TestImpersonateStopJSONNoContext asserts that stopping with no context
// emits {stopped:false} in JSON mode (no target_email key).
func TestImpersonateStopJSONNoContext(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_API_URL", "http://localhost:3000")
	var out, errOut strings.Builder
	if code := run([]string{"impersonate", "stop", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &m); err != nil {
		t.Fatalf("stdout is not valid JSON: %v; got %q", err, out.String())
	}
	if m["stopped"] != false {
		t.Errorf("stopped = %v, want false", m["stopped"])
	}
	if _, hasEmail := m["target_email"]; hasEmail {
		t.Errorf("target_email should be absent when stopped=false, got %v", m)
	}
}

// TestImpersonateStopStyledWithContext covers the stopped=true branch of
// impersonateStopView.Styled() which is not exercised by the JSON tests.
func TestImpersonateStopStyledWithContext(t *testing.T) {
	authEnv(t)
	srv := impersonateServer(t)
	t.Setenv("LK_API_URL", srv.URL)
	_ = auth.SaveImpersonation(srv.URL, auth.Impersonation{
		Token: "lkn_imp", TargetEmail: "target@linkana.com", BuyerID: "b1",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	var out, errOut strings.Builder
	if code := run([]string{"impersonate", "stop", "--format", "styled"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "encerrada") || !strings.Contains(out.String(), "target@linkana.com") {
		t.Errorf("expected encerrada message, got %q", out.String())
	}
}
