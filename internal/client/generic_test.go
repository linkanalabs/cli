package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/linkanalabs/cli/internal/mode"
)

func TestDoGetWithQueryAppendsJSONBeforeQuery(t *testing.T) {
	var gotPath, gotRawQuery, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRawQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.Token = "lkn_abc_def"
	q := url.Values{}
	q.Set("q", "acme inc")
	q.Add("tags[]", "a")
	q.Add("tags[]", "b")

	resp, err := c.Do(context.Background(), http.MethodGet, "/things", q, nil)
	if err != nil {
		t.Fatalf("Do() error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d", resp.StatusCode)
	}
	if gotPath != "/things.json" {
		t.Errorf("path = %q, want /things.json", gotPath)
	}
	parsed, err := url.ParseQuery(gotRawQuery)
	if err != nil {
		t.Fatalf("parsing query: %v", err)
	}
	if parsed.Get("q") != "acme inc" {
		t.Errorf("q = %q", parsed.Get("q"))
	}
	if got := parsed["tags[]"]; len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("tags[] = %v", got)
	}
	if gotAuth != "Bearer lkn_abc_def" {
		t.Errorf("Authorization = %q", gotAuth)
	}
}

func TestDoGetWithoutQueryOrPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/things.json" || r.URL.RawQuery != "" {
			t.Errorf("url = %q", r.URL.String())
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if _, err := New(srv.URL).Do(context.Background(), http.MethodGet, "/things", nil, nil); err != nil {
		t.Fatalf("Do() error: %v", err)
	}
}

func TestDoPatchSendsJSONBody(t *testing.T) {
	var gotMethod, gotContentType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.Mode = mode.Write
	payload := map[string]any{"root": map[string]any{"content": "hi"}}
	if _, err := c.Do(context.Background(), http.MethodPatch, "/things/1", nil, payload); err != nil {
		t.Fatalf("Do() error: %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q", gotContentType)
	}
	var decoded map[string]map[string]string
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("body not JSON: %v (%q)", err, gotBody)
	}
	if decoded["root"]["content"] != "hi" {
		t.Errorf("body = %q", gotBody)
	}
}

func TestDoNonGetBlockedInReadMode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("read-mode Do must not reach the network")
	}))
	defer srv.Close()

	c := New(srv.URL) // read mode by default
	_, err := c.Do(context.Background(), http.MethodPost, "/things", nil, map[string]any{"a": 1})
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("want ErrReadOnly, got %v", err)
	}
}

func TestDoPayloadMarshalError(t *testing.T) {
	c := New("http://example")
	c.Mode = mode.Write
	if _, err := c.Do(context.Background(), http.MethodPost, "/things", nil, make(chan int)); err == nil {
		t.Fatal("expected marshal error for unencodable payload")
	}
}
