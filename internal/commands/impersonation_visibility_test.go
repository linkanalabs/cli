package commands

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/linkanalabs/cli/internal/auth"
)

func TestWhoamiShowsImpersonationBanner(t *testing.T) {
	authEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// impersonation token resolves to the target identity
		_, _ = w.Write([]byte(`{"id":"u1","email":"suporte@linkana.com","name":"Suporte","role":"operator","buyer_id":"b1","is_staff":false}`))
	}))
	defer srv.Close()
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_original")
	_ = auth.SaveImpersonation(srv.URL, auth.Impersonation{
		Token: "lkn_imp", TargetEmail: "suporte@linkana.com", BuyerID: "b1",
		ImpersonatorEmail: "staff@linkana.com", ExpiresAt: time.Now().Add(time.Hour),
	})

	var out, errOut bytes.Buffer
	code := run([]string{"whoami", "--format", "styled"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "impersonando") || !strings.Contains(combined, "staff@linkana.com") {
		t.Errorf("whoami should surface impersonation: out=%q err=%q", out.String(), errOut.String())
	}
}

func TestAuthStatusShowsImpersonation(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_API_URL", "http://localhost:3000")
	t.Setenv("LK_TOKEN", "lkn_original")
	_ = auth.SaveImpersonation("http://localhost:3000", auth.Impersonation{
		Token: "lkn_imp", TargetEmail: "suporte@linkana.com", BuyerID: "b1",
		ImpersonatorEmail: "staff@linkana.com", ExpiresAt: time.Now().Add(time.Hour),
	})
	var out, errOut bytes.Buffer
	if code := run([]string{"auth", "status"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "suporte@linkana.com") {
		t.Errorf("auth status should show impersonation: %q", out.String())
	}
}

func TestAuthStatusShowsExpiredImpersonation(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_API_URL", "http://localhost:3000")
	t.Setenv("LK_TOKEN", "lkn_original")
	base := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	swapTimeNow(t, func() time.Time { return base })
	_ = auth.SaveImpersonation("http://localhost:3000", auth.Impersonation{
		Token: "lkn_imp", TargetEmail: "suporte@linkana.com", BuyerID: "b1",
		ImpersonatorEmail: "staff@linkana.com", ExpiresAt: base.Add(-time.Minute), // already expired
	})
	var out, errOut bytes.Buffer
	// auth status itself should still succeed (exit 0) when impersonation is expired;
	// it just marks the block EXPIRADA. The hard error path is in whoami/resolveAPI.
	if code := run([]string{"auth", "status"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "EXPIRADA") {
		t.Errorf("auth status should mark expired impersonation: %q", out.String())
	}
}
