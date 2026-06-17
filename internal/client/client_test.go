package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewTrimsTrailingSlash(t *testing.T) {
	c := New("http://localhost:3000/")
	if c.BaseURL != "http://localhost:3000" {
		t.Errorf("BaseURL = %q", c.BaseURL)
	}
}

func TestEnsureJSON(t *testing.T) {
	cases := map[string]string{
		"/buyers":          "/buyers.json",
		"/buyers.json":     "/buyers.json",
		"/buyers?page=2":   "/buyers.json?page=2",
		"/buyers.json?x=1": "/buyers.json?x=1",
	}
	for in, want := range cases {
		if got := ensureJSON(in); got != want {
			t.Errorf("ensureJSON(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildURL(t *testing.T) {
	c := New("http://host")
	if got := c.buildURL("buyers"); got != "http://host/buyers.json" {
		t.Errorf("relative path: %q", got)
	}
	if got := c.buildURL("/buyers"); got != "http://host/buyers.json" {
		t.Errorf("leading slash: %q", got)
	}
	abs := "https://other/thing.json"
	if got := c.buildURL(abs); got != abs {
		t.Errorf("absolute URL should pass through: %q", got)
	}
}

func TestGetSendsHeadersAndJSONPath(t *testing.T) {
	var gotPath, gotAccept, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAccept = r.Header.Get("Accept")
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.Token = "lkn_abc_def"

	resp, err := c.Get(context.Background(), "/buyers")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d", resp.StatusCode)
	}
	if string(resp.Body) != `{"ok":true}` {
		t.Errorf("Body = %q", resp.Body)
	}
	if gotPath != "/buyers.json" {
		t.Errorf("path = %q, want /buyers.json", gotPath)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q", gotAccept)
	}
	if gotAuth != "Bearer lkn_abc_def" {
		t.Errorf("Authorization = %q", gotAuth)
	}
}

func TestGetWithoutTokenOmitsAuth(t *testing.T) {
	var hadAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadAuth = r.Header["Authorization"]
	}))
	defer srv.Close()

	if _, err := New(srv.URL).Get(context.Background(), "/up"); err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if hadAuth {
		t.Error("Authorization header should be absent without a token")
	}
}

func TestGetRequestError(t *testing.T) {
	// Invalid control character in URL makes NewRequestWithContext fail.
	c := &Client{BaseURL: "http://host\n", HTTPClient: http.DefaultClient}
	if _, err := c.Get(context.Background(), "/x"); err == nil {
		t.Fatal("expected request build error")
	}
}

func TestGetTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // ensure the connection is refused

	c := New(url)
	if _, err := c.Get(context.Background(), "/up"); err == nil {
		t.Fatal("expected transport error against closed server")
	}
}
