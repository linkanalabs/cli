package commands

import (
	"bytes"
	"strings"
	"testing"
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
