package commands

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/linkanalabs/cli/internal/auth"
)

// runWithStdin runs the CLI with the given stdin contents.
func runWithStdin(t *testing.T, args []string, stdin string, stdout, stderr *bytes.Buffer) int {
	t.Helper()
	return runWith(strings.NewReader(stdin), args, stdout, stderr)
}

// authEnv isolates config + token storage to temp dirs with the keyring off.
func authEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LK_NO_KEYRING", "1")
	t.Setenv("LK_TOKEN", "")
	t.Setenv("LK_API_URL", "http://localhost:3000")
}

func TestAuthLoginWithTokenFlag(t *testing.T) {
	authEnv(t)
	var out, errOut bytes.Buffer
	code := run([]string{"auth", "login", "--token", "lkn_abc_def", "--format", "json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"saved"`) && !strings.Contains(out.String(), "saved") {
		t.Errorf("output = %q", out.String())
	}
	// The secret must never be printed.
	if strings.Contains(out.String(), "lkn_abc_def") {
		t.Errorf("login output leaked the token: %q", out.String())
	}
}

func TestAuthLoginFromEnv(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_TOKEN", "lkn_env_token")
	var out, errOut bytes.Buffer
	code := run([]string{"auth", "login", "--format", "json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
}

func TestAuthLoginRejectsMalformed(t *testing.T) {
	authEnv(t)
	var out, errOut bytes.Buffer
	code := run([]string{"auth", "login", "--token", "not-a-token"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "error:") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestAuthLoginNoTokenAvailable(t *testing.T) {
	authEnv(t)
	var out, errOut bytes.Buffer
	// No --token, no env, and stdin is not a terminal (empty) → error.
	code := runWithStdin(t, []string{"auth", "login"}, "", &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1, stderr=%q", code, errOut.String())
	}
}

func TestAuthLoginFromStdin(t *testing.T) {
	authEnv(t)
	var out, errOut bytes.Buffer
	code := runWithStdin(t, []string{"auth", "login", "--format", "json"}, "lkn_stdin_token\n", &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
}

func TestAuthStatusNoToken(t *testing.T) {
	authEnv(t)
	var out, errOut bytes.Buffer
	code := run([]string{"auth", "status", "--format", "json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"authenticated": false`) {
		t.Errorf("output = %q", out.String())
	}
}

func TestAuthStatusWithToken(t *testing.T) {
	authEnv(t)
	// login first
	var b1, e1 bytes.Buffer
	if code := run([]string{"auth", "login", "--token", "lkn_abc_def"}, &b1, &e1); code != 0 {
		t.Fatalf("login failed: %q", e1.String())
	}
	var out, errOut bytes.Buffer
	code := run([]string{"auth", "status", "--format", "json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"authenticated": true`) {
		t.Errorf("output = %q", out.String())
	}
	if !strings.Contains(out.String(), `"file"`) {
		t.Errorf("expected file source, output = %q", out.String())
	}
	if strings.Contains(out.String(), "lkn_abc_def") {
		t.Errorf("status leaked the token: %q", out.String())
	}
}

func TestAuthStatusEnvSource(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_TOKEN", "lkn_env_token")
	var out, errOut bytes.Buffer
	code := run([]string{"auth", "status", "--format", "json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), `"env"`) {
		t.Errorf("expected env source, output = %q", out.String())
	}
}

func TestAuthLogout(t *testing.T) {
	authEnv(t)
	var b1, e1 bytes.Buffer
	if code := run([]string{"auth", "login", "--token", "lkn_abc_def"}, &b1, &e1); code != 0 {
		t.Fatalf("login failed: %q", e1.String())
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"auth", "logout", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("logout exit = %d, stderr = %q", code, errOut.String())
	}
	// Status should now report no token.
	var b2, e2 bytes.Buffer
	run([]string{"auth", "status", "--format", "json"}, &b2, &e2)
	if !strings.Contains(b2.String(), `"authenticated": false`) {
		t.Errorf("after logout status = %q", b2.String())
	}
}

func TestStatusResultStyledNotAuthenticated(t *testing.T) {
	r := statusResult{Authenticated: false, BaseURL: "http://x"}
	if !strings.Contains(r.Styled(), "Not authenticated") {
		t.Errorf("styled = %q", r.Styled())
	}
}

func TestAuthStyledOutput(t *testing.T) {
	authEnv(t)
	var b1, e1 bytes.Buffer
	run([]string{"auth", "login", "--token", "lkn_abc_def"}, &b1, &e1)

	var out, errOut bytes.Buffer
	run([]string{"auth", "status", "--format", "styled"}, &out, &errOut)
	if !strings.Contains(out.String(), "Authenticated") {
		t.Errorf("styled status = %q", out.String())
	}

	var lo, le bytes.Buffer
	run([]string{"auth", "logout", "--format", "styled"}, &lo, &le)
	if lo.Len() == 0 {
		t.Error("styled logout produced no output")
	}

	var li, lie bytes.Buffer
	run([]string{"auth", "login", "--token", "lkn_x_y", "--format", "styled"}, &li, &lie)
	if !strings.Contains(li.String(), "Token saved") {
		t.Errorf("styled login = %q", li.String())
	}
}

// --- Finding C: auth status --format json includes impersonation block ---

// TestAuthStatusJSONImpersonationBlock asserts that `auth status --format json`
// includes the impersonation block with correct fields and no token.
func TestAuthStatusJSONImpersonationBlock(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_TOKEN", "lkn_orig_tok")

	expires := time.Now().Add(2 * time.Hour).UTC().Round(time.Second)
	_ = auth.SaveImpersonation("http://localhost:3000", auth.Impersonation{
		Token:             "lkn_secret_imp",
		TargetEmail:       "buyer@linkana.com",
		BuyerID:           "b42",
		ImpersonatorEmail: "staff@linkana.com",
		ExpiresAt:         expires,
	})
	swapTimeNow(t, func() time.Time { return time.Now() })

	var out, errOut bytes.Buffer
	if code := run([]string{"auth", "status", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	raw := strings.TrimSpace(out.String())
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("stdout is not valid JSON: %v; got %q", err, raw)
	}
	// Must contain impersonation sub-object.
	imp, ok := m["impersonation"].(map[string]any)
	if !ok || imp == nil {
		t.Fatalf("impersonation block missing or wrong type, got %v", m["impersonation"])
	}
	if imp["target_email"] != "buyer@linkana.com" {
		t.Errorf("impersonation.target_email = %v", imp["target_email"])
	}
	if imp["buyer_id"] != "b42" {
		t.Errorf("impersonation.buyer_id = %v", imp["buyer_id"])
	}
	// expired must be present and false (context is active).
	if imp["expired"] != false {
		t.Errorf("impersonation.expired = %v, want false", imp["expired"])
	}
	// Token must never appear anywhere in the output.
	if strings.Contains(raw, "lkn_secret_imp") {
		t.Errorf("token leaked in JSON output: %q", raw)
	}
}

// TestAuthStatusJSONWithoutImpersonation asserts that without an active context
// the impersonation key is absent (omitempty) and JSON is still valid.
func TestAuthStatusJSONWithoutImpersonation(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_TOKEN", "lkn_orig_tok")

	var out, errOut bytes.Buffer
	if code := run([]string{"auth", "status", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	raw := strings.TrimSpace(out.String())
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("stdout is not valid JSON: %v; got %q", err, raw)
	}
	if _, present := m["impersonation"]; present {
		t.Errorf("impersonation key should be absent (omitempty), got %v", m)
	}
}

// TestAuthStatusAutoFormatPipedStaysJSON guards the format-resolution bug: in
// `--format auto` to a non-TTY (piped), output must resolve to JSON and stay a
// single valid JSON document — no out-of-band human line appended.
func TestAuthStatusAutoFormatPipedStaysJSON(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_TOKEN", "lkn_orig_tok")
	_ = auth.SaveImpersonation("http://localhost:3000", auth.Impersonation{
		Token:             "lkn_secret_imp",
		TargetEmail:       "buyer@linkana.com",
		BuyerID:           "b42",
		ImpersonatorEmail: "staff@linkana.com",
		ExpiresAt:         time.Now().Add(time.Hour),
	})
	swapTimeNow(t, func() time.Time { return time.Now() })

	// No --format flag → auto; out is a bytes.Buffer (non-TTY) → resolves to JSON.
	var out, errOut bytes.Buffer
	if code := run([]string{"auth", "status"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	raw := strings.TrimSpace(out.String())
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("auto+piped stdout must be valid JSON, got %q: %v", raw, err)
	}
	if strings.Contains(raw, "impersonação (") {
		t.Errorf("human impersonation line leaked into JSON output: %q", raw)
	}
}

// TestAuthStatusJSONExpiredImpersonation asserts that an expired context sets
// expired:true in the impersonation block.
func TestAuthStatusJSONExpiredImpersonation(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_TOKEN", "lkn_orig_tok")

	past := time.Now().Add(-time.Hour)
	_ = auth.SaveImpersonation("http://localhost:3000", auth.Impersonation{
		Token: "lkn_exp_tok", TargetEmail: "old@linkana.com", BuyerID: "b1",
		ExpiresAt: past,
	})
	swapTimeNow(t, func() time.Time { return time.Now() })

	var out, errOut bytes.Buffer
	if code := run([]string{"auth", "status", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &m); err != nil {
		t.Fatalf("stdout is not valid JSON: %v; got %q", err, out.String())
	}
	imp, ok := m["impersonation"].(map[string]any)
	if !ok {
		t.Fatalf("impersonation block missing, got %v", m)
	}
	if imp["expired"] != true {
		t.Errorf("impersonation.expired = %v, want true", imp["expired"])
	}
	// Token must never appear.
	if strings.Contains(out.String(), "lkn_exp_tok") {
		t.Errorf("token leaked: %q", out.String())
	}
}

// --- Finding D: auth status warns on LoadImpersonation error ---

// impersonationTokenPath returns the file path where auth.SaveImpersonation stores
// the impersonation blob for the given base URL. It mirrors the internal logic of
// the auth package's file-fallback store so tests can corrupt the file directly.
func impersonationTokenPath(t *testing.T, baseURL string) string {
	t.Helper()
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		t.Skip("XDG_CONFIG_HOME not set; cannot compute token path")
	}
	// Mirror: origin = baseURL + "|impersonation"; file = sha256(origin)[:8] + ".token"
	origin := baseURL + "|impersonation"
	sum := sha256.Sum256([]byte(origin))
	filename := hex.EncodeToString(sum[:8]) + ".token"
	return filepath.Join(xdg, "lk", "tokens", filename)
}

// TestAuthStatusWarnsOnImpersonationLoadError asserts that a LoadImpersonation
// error (corrupt file) causes a warning on stderr but does NOT fail the command.
func TestAuthStatusWarnsOnImpersonationLoadError(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_TOKEN", "lkn_orig_tok")

	// First save a valid impersonation so the dir is created.
	_ = auth.SaveImpersonation("http://localhost:3000", auth.Impersonation{
		Token: "lkn_tmp", TargetEmail: "x@x.com", BuyerID: "b1",
		ExpiresAt: time.Now().Add(time.Hour),
	})

	// Overwrite with invalid JSON to trigger a parse error in LoadImpersonation.
	impFile := impersonationTokenPath(t, "http://localhost:3000")
	if err := os.WriteFile(impFile, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("failed to corrupt file: %v", err)
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"auth", "status", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d (must be 0 even on impersonation load error), stderr = %q", code, errOut.String())
	}
	// A warning must appear on stderr.
	if !strings.Contains(errOut.String(), "aviso") {
		t.Errorf("expected aviso warning on stderr, got %q", errOut.String())
	}
	// Base status must still be valid JSON.
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &m); err != nil {
		t.Fatalf("stdout is not valid JSON: %v; got %q", err, out.String())
	}
}

// TestAuthStatusAuthenticatedViaActiveImpersonation: an active (non-expired)
// impersonation reports authenticated even with no base token — the impersonation
// token takes precedence and is usable.
func TestAuthStatusAuthenticatedViaActiveImpersonation(t *testing.T) {
	authEnv(t) // no LK_TOKEN, no stored token => base token absent
	future := time.Now().Add(time.Hour)
	_ = auth.SaveImpersonation("http://localhost:3000", auth.Impersonation{
		Token: "lkn_imp", TargetEmail: "b@linkana.com", BuyerID: "b1", ExpiresAt: future,
	})
	swapTimeNow(t, func() time.Time { return time.Now() })

	var out, errOut bytes.Buffer
	if code := run([]string{"auth", "status", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &m); err != nil {
		t.Fatalf("invalid JSON: %v; got %q", err, out.String())
	}
	if m["authenticated"] != true {
		t.Errorf("authenticated = %v, want true (active impersonation, no base token)", m["authenticated"])
	}
}

// TestAuthStatusNotAuthenticatedWhenImpersonationExpired: an expired impersonation
// is sticky (hard error, no fallback), so status reports not authenticated even
// when a base token exists.
func TestAuthStatusNotAuthenticatedWhenImpersonationExpired(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_TOKEN", "lkn_orig_tok")
	past := time.Now().Add(-time.Hour)
	_ = auth.SaveImpersonation("http://localhost:3000", auth.Impersonation{
		Token: "lkn_exp", TargetEmail: "old@linkana.com", BuyerID: "b1", ExpiresAt: past,
	})
	swapTimeNow(t, func() time.Time { return time.Now() })

	var out, errOut bytes.Buffer
	if code := run([]string{"auth", "status", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &m); err != nil {
		t.Fatalf("invalid JSON: %v; got %q", err, out.String())
	}
	if m["authenticated"] != false {
		t.Errorf("authenticated = %v, want false (expired impersonation)", m["authenticated"])
	}
}
