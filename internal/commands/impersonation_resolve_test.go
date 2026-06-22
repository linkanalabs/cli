package commands

import (
	"strings"
	"testing"
	"time"

	"github.com/linkanalabs/cli/internal/auth"
)

// NOTE: adjust the module path import above to match go.mod if different.

func TestResolveAPIUsesOriginalWhenNoImpersonation(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_TOKEN", "lkn_original")
	api, imp, err := resolveAPI()
	if err != nil {
		t.Fatalf("resolveAPI() error: %v", err)
	}
	if imp != nil {
		t.Errorf("imp = %+v, want nil", imp)
	}
	if api == nil {
		t.Fatal("api = nil")
	}
}

func TestResolveAPIUsesImpersonationWhenActive(t *testing.T) {
	authEnv(t)
	// Store original token via Save (not LK_TOKEN) so LoadImpersonation is not
	// short-circuited by the env override in auth.Load.
	if err := auth.Save("http://localhost:3000", "lkn_original"); err != nil {
		t.Fatalf("auth.Save: %v", err)
	}
	base := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	swapTimeNow(t, func() time.Time { return base })
	cfg := "http://localhost:3000"
	_ = auth.SaveImpersonation(cfg, auth.Impersonation{
		Token: "lkn_imp", TargetEmail: "s@linkana.com", BuyerID: "b1",
		ExpiresAt: base.Add(time.Hour),
	})
	_, imp, err := resolveAPI()
	if err != nil {
		t.Fatalf("resolveAPI() error: %v", err)
	}
	if imp == nil || imp.TargetEmail != "s@linkana.com" {
		t.Fatalf("imp = %+v", imp)
	}
}

func TestResolveAPIHardErrorsWhenImpersonationExpired(t *testing.T) {
	authEnv(t)
	// Store original token via Save (not LK_TOKEN) so LoadImpersonation is not
	// short-circuited by the env override in auth.Load.
	if err := auth.Save("http://localhost:3000", "lkn_original"); err != nil {
		t.Fatalf("auth.Save: %v", err)
	}
	base := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	swapTimeNow(t, func() time.Time { return base })
	cfg := "http://localhost:3000"
	_ = auth.SaveImpersonation(cfg, auth.Impersonation{
		Token: "lkn_imp", TargetEmail: "s@linkana.com", BuyerID: "b1",
		ExpiresAt: base.Add(-time.Minute), // already expired
	})
	_, _, err := resolveAPI()
	if err == nil {
		t.Fatal("expected hard error on expired impersonation")
	}
	msg := err.Error()
	for _, want := range []string{"expirou", "lk impersonate stop", "s@linkana.com"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q: %q", want, msg)
		}
	}
}

func TestUnauthorizedErrWithoutImpersonation(t *testing.T) {
	if got := unauthorizedErr(nil).Error(); !strings.Contains(got, "lk auth login") {
		t.Errorf("got %q, want auth login hint", got)
	}
}

func TestUnauthorizedErrWithImpersonation(t *testing.T) {
	err := unauthorizedErr(&auth.Impersonation{TargetEmail: "s@linkana.com", BuyerID: "b1"})
	for _, want := range []string{"impersonação", "lk impersonate stop", "s@linkana.com"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %q", want, err.Error())
		}
	}
}
