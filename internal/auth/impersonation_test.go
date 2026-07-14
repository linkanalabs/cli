package auth

import (
	"testing"
	"time"
)

func impersonationEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(EnvNoKeyring, "1")
	t.Setenv(EnvToken, "")
}

func TestSaveLoadImpersonationRoundTrip(t *testing.T) {
	impersonationEnv(t)
	origin := "http://localhost:3000"
	exp := time.Date(2026, 6, 23, 14, 0, 0, 0, time.UTC)
	in := Impersonation{
		Token:             "lkn_imp_tok",
		TargetEmail:       "suporte@linkana.com",
		TargetUserID:      "u1",
		BuyerID:           "b1",
		ImpersonatorEmail: "staff@linkana.com",
		ExpiresAt:         exp,
	}
	if err := SaveImpersonation(origin, in); err != nil {
		t.Fatalf("SaveImpersonation() error: %v", err)
	}
	got, err := LoadImpersonation(origin)
	if err != nil {
		t.Fatalf("LoadImpersonation() error: %v", err)
	}
	if got == nil {
		t.Fatal("LoadImpersonation() = nil, want context")
	}
	if got.Token != in.Token || got.TargetEmail != in.TargetEmail || got.BuyerID != in.BuyerID ||
		got.TargetUserID != in.TargetUserID || got.ImpersonatorEmail != in.ImpersonatorEmail {
		t.Errorf("got = %+v, want %+v", *got, in)
	}
	if !got.ExpiresAt.Equal(exp) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, exp)
	}
}

func TestLoadImpersonationAbsent(t *testing.T) {
	impersonationEnv(t)
	got, err := LoadImpersonation("http://localhost:3000")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got != nil {
		t.Errorf("got = %+v, want nil", got)
	}
}

func TestDeleteImpersonation(t *testing.T) {
	impersonationEnv(t)
	origin := "http://localhost:3000"
	_ = SaveImpersonation(origin, Impersonation{Token: "lkn_imp_tok", ExpiresAt: time.Now()})
	if err := DeleteImpersonation(origin); err != nil {
		t.Fatalf("DeleteImpersonation() error: %v", err)
	}
	got, _ := LoadImpersonation(origin)
	if got != nil {
		t.Errorf("after delete got = %+v, want nil", got)
	}
}

func TestImpersonationExpired(t *testing.T) {
	base := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	imp := Impersonation{ExpiresAt: base}
	if imp.Expired(base.Add(-time.Second)) {
		t.Error("should not be expired one second before ExpiresAt")
	}
	if !imp.Expired(base.Add(time.Second)) {
		t.Error("should be expired one second after ExpiresAt")
	}
}

func TestImpersonationIsolatedFromToken(t *testing.T) {
	impersonationEnv(t)
	origin := "http://localhost:3000"
	if err := Save(origin, "lkn_original"); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	_ = SaveImpersonation(origin, Impersonation{Token: "lkn_imp", ExpiresAt: time.Now()})
	// Saving impersonation must not clobber the original token.
	tok, _, err := Load(origin)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if tok != "lkn_original" {
		t.Errorf("original token = %q, want lkn_original", tok)
	}
}

// TestLoadImpersonationNotMaskedByEnvToken proves that an active stored
// impersonation context is NOT silently ignored when LK_TOKEN is set.
// This is the key safety property: LK_TOKEN overrides the original token only;
// impersonation context (which is sticky and explicit) must take precedence.
func TestLoadImpersonationNotMaskedByEnvToken(t *testing.T) {
	// Use file store (no keyring), with a temp XDG dir.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(EnvNoKeyring, "1")
	// LK_TOKEN is set (simulating an operator or CI environment).
	t.Setenv(EnvToken, "lkn_env_original")

	origin := "http://localhost:3000"
	imp := Impersonation{
		Token:             "lkn_imp_tok",
		TargetEmail:       "target@linkana.com",
		TargetUserID:      "u42",
		BuyerID:           "b99",
		ImpersonatorEmail: "staff@linkana.com",
		ExpiresAt:         time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := SaveImpersonation(origin, imp); err != nil {
		t.Fatalf("SaveImpersonation() error: %v", err)
	}

	got, err := LoadImpersonation(origin)
	if err != nil {
		t.Fatalf("LoadImpersonation() error: %v", err)
	}
	if got == nil {
		t.Fatal("LoadImpersonation() = nil; LK_TOKEN must not mask a stored impersonation context")
	}
	if got.Token != "lkn_imp_tok" {
		t.Errorf("got.Token = %q, want lkn_imp_tok", got.Token)
	}
	if got.TargetEmail != "target@linkana.com" {
		t.Errorf("got.TargetEmail = %q, want target@linkana.com", got.TargetEmail)
	}
}
