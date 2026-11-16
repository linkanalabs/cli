package manifest

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const remoteManifest = `{"manifest_version":1,"generated_at":"2026-11-16T00:00:00Z","source":"linkanalabs/linkana@remote",
"endpoints":[{"command":["thing","list"],"summary":"List things","method":"GET","path":"/things","path_params":[],"params":[]}]}`

// backend serves remoteManifest with an ETag, answers 304 to that ETag, and
// counts requests and remembers the headers of the last one.
type backend struct {
	server  *httptest.Server
	hits    atomic.Int32
	body    string
	status  int
	lastReq http.Header
}

func newBackend(t *testing.T) *backend {
	t.Helper()
	b := &backend{body: remoteManifest, status: http.StatusOK}
	b.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.hits.Add(1)
		b.lastReq = r.Header.Clone()
		if r.URL.Path != RemotePath {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("If-None-Match") == `"v1"` && b.status == http.StatusOK {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.WriteHeader(b.status)
		_, _ = w.Write([]byte(b.body))
	}))
	t.Cleanup(b.server.Close)
	return b
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newRemote(t *testing.T, b *backend, c *clock) (*Remote, *bytes.Buffer) {
	t.Helper()
	warn := &bytes.Buffer{}
	return &Remote{
		BaseURL:  b.server.URL,
		Token:    "lkn_abc_def",
		CacheDir: t.TempDir(),
		Now:      c.now,
		Warn:     warn,
	}, warn
}

func resolve(t *testing.T, r *Remote, refresh bool) *Manifest {
	t.Helper()
	m, err := r.Resolve(context.Background(), refresh)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return m
}

func TestRemoteFetchesStoresAndSendsTheToken(t *testing.T) {
	b := newBackend(t)
	r, _ := newRemote(t, b, &clock{t: time.Now()})

	m := resolve(t, r, true)

	if m.Origin != OriginBackend || m.Source != "linkanalabs/linkana@remote" {
		t.Fatalf("got origin %q source %q", m.Origin, m.Source)
	}
	if got := b.lastReq.Get("Authorization"); got != "Bearer lkn_abc_def" {
		t.Errorf("Authorization = %q", got)
	}
	if _, err := os.Stat(filepath.Join(r.dir(), "manifest.json")); err != nil {
		t.Errorf("manifest not cached: %v", err)
	}
}

func TestRemoteTrustsTheCopyUntilMaxAge(t *testing.T) {
	b := newBackend(t)
	c := &clock{t: time.Now()}
	r, _ := newRemote(t, b, c)
	resolve(t, r, true)

	c.t = c.t.Add(DefaultMaxAge - time.Second)
	m := resolve(t, r, true)

	if b.hits.Load() != 1 {
		t.Errorf("backend asked %d times, want 1", b.hits.Load())
	}
	if m.Origin != OriginCache {
		t.Errorf("origin = %q, want cache", m.Origin)
	}
}

func TestRemoteRevalidatesWithTheETagAfterMaxAge(t *testing.T) {
	b := newBackend(t)
	c := &clock{t: time.Now()}
	r, _ := newRemote(t, b, c)
	resolve(t, r, true)

	c.t = c.t.Add(DefaultMaxAge)
	m := resolve(t, r, true)

	if got := b.lastReq.Get("If-None-Match"); got != `"v1"` {
		t.Errorf("If-None-Match = %q", got)
	}
	if m.Origin != OriginBackend || len(m.Endpoints) != 1 {
		t.Errorf("origin %q with %d endpoints", m.Origin, len(m.Endpoints))
	}
}

func TestRemoteNeverAsksWithoutRefreshTokenOrBaseURL(t *testing.T) {
	b := newBackend(t)
	r, _ := newRemote(t, b, &clock{t: time.Now()})

	if m := resolve(t, r, false); m.Origin != OriginEmbedded {
		t.Errorf("without refresh: origin %q", m.Origin)
	}
	r.Token = ""
	if m := resolve(t, r, true); m.Origin != OriginEmbedded {
		t.Errorf("without token: origin %q", m.Origin)
	}
	r.Token, r.BaseURL = "lkn_abc_def", ""
	if m := resolve(t, r, true); m.Origin != OriginEmbedded {
		t.Errorf("without base url: origin %q", m.Origin)
	}
	if b.hits.Load() != 0 {
		t.Errorf("backend asked %d times", b.hits.Load())
	}
}

func TestRemoteFallsBackToTheCopyWhenTheBackendIsDown(t *testing.T) {
	b := newBackend(t)
	c := &clock{t: time.Now()}
	r, _ := newRemote(t, b, c)
	resolve(t, r, true)

	b.server.Close()
	c.t = c.t.Add(DefaultMaxAge)
	m := resolve(t, r, true)

	if m.Origin != OriginCache || m.Source != "linkanalabs/linkana@remote" {
		t.Errorf("origin %q source %q", m.Origin, m.Source)
	}
}

func TestRemoteWaitsMaxAgeBeforeRetryingAFailedCheck(t *testing.T) {
	b := newBackend(t)
	b.status = http.StatusInternalServerError
	c := &clock{t: time.Now()}
	r, _ := newRemote(t, b, c)

	if m := resolve(t, r, true); m.Origin != OriginEmbedded {
		t.Errorf("origin %q", m.Origin)
	}
	resolve(t, r, true)

	if b.hits.Load() != 1 {
		t.Errorf("backend asked %d times, want 1", b.hits.Load())
	}
}

func TestRemoteKeepsTheCopyWhenTheBackendManifestIsUnreadable(t *testing.T) {
	b := newBackend(t)
	c := &clock{t: time.Now()}
	r, warn := newRemote(t, b, c)
	resolve(t, r, true)

	b.body = strings.Replace(remoteManifest, `"params":[]`, `"params":[{"name":"x","type":"vector","in":"query"}]`, 1)
	r.Token = "lkn_other"
	c.t = c.t.Add(DefaultMaxAge)
	r.BaseURL = b.server.URL
	writeMetaETag(t, r, `"v0"`)
	m := resolve(t, r, true)

	if m.Origin != OriginCache || m.Source != "linkanalabs/linkana@remote" {
		t.Errorf("origin %q source %q", m.Origin, m.Source)
	}
	if !strings.Contains(warn.String(), "not readable by this lk version") || !strings.Contains(warn.String(), "lk update") {
		t.Errorf("warning = %q", warn.String())
	}
}

func TestRemoteIgnoresABrokenCopyAndCopiesOfAnotherBaseURL(t *testing.T) {
	b := newBackend(t)
	r, _ := newRemote(t, b, &clock{t: time.Now()})
	resolve(t, r, true)

	if err := os.WriteFile(filepath.Join(r.dir(), "manifest.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if m := resolve(t, r, false); m.Origin != OriginEmbedded {
		t.Errorf("broken copy: origin %q", m.Origin)
	}

	other := *r
	other.BaseURL = "https://staging.example"
	if m := resolve(t, &other, false); m.Origin != OriginEmbedded {
		t.Errorf("other base url: origin %q", m.Origin)
	}
}

func TestRemoteDoesNotFetchWithoutACacheDirAndStillWorks(t *testing.T) {
	b := newBackend(t)
	r, _ := newRemote(t, b, &clock{t: time.Now()})
	r.CacheDir = ""

	m := resolve(t, r, true)

	if m.Origin != OriginEmbedded {
		t.Errorf("origin %q", m.Origin)
	}
}

func TestRemoteRejectsAnInvalidBaseURL(t *testing.T) {
	r := &Remote{BaseURL: "://bad", Token: "lkn_abc_def", CacheDir: t.TempDir()}

	if m := resolve(t, r, true); m.Origin != OriginEmbedded {
		t.Errorf("origin %q", m.Origin)
	}
}

func TestRemoteFallsBackWhenTheCacheCannotBeWritten(t *testing.T) {
	b := newBackend(t)
	r, _ := newRemote(t, b, &clock{t: time.Now()})
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r.CacheDir = blocker

	if m := resolve(t, r, true); m.Origin != OriginEmbedded {
		t.Errorf("origin %q", m.Origin)
	}
}

func TestRemoteHonoursMaxAgeAndHTTPClient(t *testing.T) {
	b := newBackend(t)
	c := &clock{t: time.Now()}
	r, _ := newRemote(t, b, c)
	r.MaxAge = time.Hour
	r.HTTP = b.server.Client()
	resolve(t, r, true)

	c.t = c.t.Add(DefaultMaxAge)
	resolve(t, r, true)

	if b.hits.Load() != 1 {
		t.Errorf("backend asked %d times, want 1", b.hits.Load())
	}
}

func TestRemoteDefaults(t *testing.T) {
	r := &Remote{}
	if r.maxAge() != DefaultMaxAge || r.httpClient() != http.DefaultClient || r.now().IsZero() {
		t.Errorf("defaults not applied")
	}
	r.warnf("ignored without a writer")
}

func writeMetaETag(t *testing.T, r *Remote, etag string) {
	t.Helper()
	if err := r.writeMeta(cacheMeta{BaseURL: r.BaseURL, ETag: etag, CheckedAt: time.Time{}}); err != nil {
		t.Fatal(err)
	}
}
