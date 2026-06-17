package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetIdentitySuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/my/identity.json" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"u_1","email":"a@b.com","name":"Ana","role":"admin","buyer_id":null,"is_staff":true}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.Token = "lkn_abc_def"
	id, err := c.GetIdentity(context.Background())
	if err != nil {
		t.Fatalf("GetIdentity() error: %v", err)
	}
	if id.ID != "u_1" || id.Email != "a@b.com" || id.Name != "Ana" || id.Role != "admin" {
		t.Errorf("identity = %+v", id)
	}
	if id.BuyerID != nil {
		t.Errorf("BuyerID = %v, want nil", id.BuyerID)
	}
	if !id.IsStaff {
		t.Error("IsStaff = false, want true")
	}
}

func TestGetIdentityWithBuyer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"u_2","email":"b@c.com","name":"Bo","role":"member","buyer_id":"b_9","is_staff":false}`))
	}))
	defer srv.Close()

	id, err := New(srv.URL).GetIdentity(context.Background())
	if err != nil {
		t.Fatalf("GetIdentity() error: %v", err)
	}
	if id.BuyerID == nil || *id.BuyerID != "b_9" {
		t.Errorf("BuyerID = %v, want b_9", id.BuyerID)
	}
}

func TestGetIdentityUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := New(srv.URL).GetIdentity(context.Background())
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("error = %v, want ErrUnauthorized", err)
	}
}

func TestGetIdentityServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, err := New(srv.URL).GetIdentity(context.Background()); err == nil {
		t.Error("expected error on 500")
	}
}

func TestGetIdentityBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	if _, err := New(srv.URL).GetIdentity(context.Background()); err == nil {
		t.Error("expected JSON decode error")
	}
}

func TestGetIdentityTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	if _, err := New(url).GetIdentity(context.Background()); err == nil {
		t.Error("expected transport error")
	}
}
