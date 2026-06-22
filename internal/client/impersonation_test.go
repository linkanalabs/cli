package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStartImpersonationSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/impersonation.json" {
			t.Errorf("method/path = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer lkn_orig_tok" {
			t.Errorf("auth header = %q", got)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["target"] != "suporte@linkana.com" {
			t.Errorf("target = %v", body["target"])
		}
		if body["ttl_seconds"].(float64) != 3600 {
			t.Errorf("ttl_seconds = %v", body["ttl_seconds"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"token":"lkn_imp_tok","identity":{"user_id":"u1","email":"suporte@linkana.com","buyer_id":"b1"},"expires_at":"2026-06-23T14:00:00Z"}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.Token = "lkn_orig_tok"
	imp, err := c.StartImpersonation(context.Background(), "suporte@linkana.com", time.Hour)
	if err != nil {
		t.Fatalf("StartImpersonation() error: %v", err)
	}
	if imp.Token != "lkn_imp_tok" || imp.Identity.Email != "suporte@linkana.com" || imp.Identity.BuyerID != "b1" {
		t.Errorf("imp = %+v", imp)
	}
	if !imp.ExpiresAt.Equal(time.Date(2026, 6, 23, 14, 0, 0, 0, time.UTC)) {
		t.Errorf("expires_at = %v", imp.ExpiresAt)
	}
}

func TestStartImpersonationOmitsZeroTTL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, present := body["ttl_seconds"]; present {
			t.Errorf("ttl_seconds should be omitted, got %v", body["ttl_seconds"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"token":"lkn_imp_tok","identity":{"user_id":"u1","email":"x@linkana.com","buyer_id":"b1"},"expires_at":"2026-06-23T14:00:00Z"}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	if _, err := c.StartImpersonation(context.Background(), "x@linkana.com", 0); err != nil {
		t.Fatalf("error: %v", err)
	}
}

func TestStartImpersonationUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if _, err := New(srv.URL).StartImpersonation(context.Background(), "x@linkana.com", 0); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("error = %v, want ErrUnauthorized", err)
	}
}

func TestStartImpersonationServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"destino inválido"}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL).StartImpersonation(context.Background(), "x@cliente.com", 0)
	if err == nil {
		t.Fatal("expected error on 422")
	}
	if got := err.Error(); !strings.Contains(got, "destino inválido") {
		t.Errorf("error should surface server message, got %q", got)
	}
}

func TestStartImpersonationBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	if _, err := New(srv.URL).StartImpersonation(context.Background(), "x@linkana.com", 0); err == nil {
		t.Error("expected decode error")
	}
}

func TestStopImpersonationSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/impersonation.json" {
			t.Errorf("method/path = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.Token = "lkn_imp_tok"
	if err := c.StopImpersonation(context.Background()); err != nil {
		t.Fatalf("StopImpersonation() error: %v", err)
	}
}

func TestStopImpersonationUnauthorizedIsOK(t *testing.T) {
	// A revoked/expired token returns 401 on DELETE; stop must treat that as success
	// so the CLI can always clear local state.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if err := New(srv.URL).StopImpersonation(context.Background()); err != nil {
		t.Errorf("StopImpersonation() on 401 should be nil, got %v", err)
	}
}
