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
