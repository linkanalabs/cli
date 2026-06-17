package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

const supplierBody = `{"id":"s_1","name":"Acme","identifier":"12345","legal_entity":"Acme Ltda","state":"active","tags":[{"id":"t_1","display_name":"Critical"}]}`

func TestListSuppliersSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/srm/suppliers.json" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer lkn_abc_def" {
			t.Errorf("auth header = %q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[" + supplierBody + "]"))
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.Token = "lkn_abc_def"
	suppliers, err := c.ListSuppliers(context.Background())
	if err != nil {
		t.Fatalf("ListSuppliers() error: %v", err)
	}
	if len(suppliers) != 1 {
		t.Fatalf("len = %d, want 1", len(suppliers))
	}
	s := suppliers[0]
	if s.ID != "s_1" || s.Name != "Acme" || s.Identifier != "12345" || s.LegalEntity != "Acme Ltda" || s.State != "active" {
		t.Errorf("supplier = %+v", s)
	}
	if len(s.Tags) != 1 || s.Tags[0].ID != "t_1" || s.Tags[0].DisplayName != "Critical" {
		t.Errorf("tags = %+v", s.Tags)
	}
}

func TestListSuppliersUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if _, err := New(srv.URL).ListSuppliers(context.Background()); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("error = %v, want ErrUnauthorized", err)
	}
}

func TestListSuppliersServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, err := New(srv.URL).ListSuppliers(context.Background()); err == nil {
		t.Error("expected error on 500")
	}
}

func TestListSuppliersBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	if _, err := New(srv.URL).ListSuppliers(context.Background()); err == nil {
		t.Error("expected JSON decode error")
	}
}

func TestListSuppliersTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	if _, err := New(url).ListSuppliers(context.Background()); err == nil {
		t.Error("expected transport error")
	}
}

func TestGetSupplierSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/srm/suppliers/s_1/panel.json" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer lkn_abc_def" {
			t.Errorf("auth header = %q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(supplierBody))
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.Token = "lkn_abc_def"
	s, err := c.GetSupplier(context.Background(), "s_1")
	if err != nil {
		t.Fatalf("GetSupplier() error: %v", err)
	}
	if s.ID != "s_1" || s.Name != "Acme" || s.State != "active" {
		t.Errorf("supplier = %+v", s)
	}
	if len(s.Tags) != 1 || s.Tags[0].DisplayName != "Critical" {
		t.Errorf("tags = %+v", s.Tags)
	}
}

func TestGetSupplierUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if _, err := New(srv.URL).GetSupplier(context.Background(), "s_1"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("error = %v, want ErrUnauthorized", err)
	}
}

func TestGetSupplierServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, err := New(srv.URL).GetSupplier(context.Background(), "s_1"); err == nil {
		t.Error("expected error on 500")
	}
}

func TestGetSupplierBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	if _, err := New(srv.URL).GetSupplier(context.Background(), "s_1"); err == nil {
		t.Error("expected JSON decode error")
	}
}

func TestGetSupplierTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	if _, err := New(url).GetSupplier(context.Background(), "s_1"); err == nil {
		t.Error("expected transport error")
	}
}
