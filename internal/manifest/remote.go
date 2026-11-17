package manifest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Where the manifest in use came from, reported by `lk version`.
const (
	OriginEmbedded = "embedded"
	OriginCache    = "cache"
	OriginBackend  = "backend"
)

// RemotePath is the backend endpoint that serves the deployed manifest.
const RemotePath = "/cli/manifest.json"

// Defaults for Remote: how long a copy is trusted before asking the backend
// again, how long that question may take, and the largest body accepted.
const (
	DefaultMaxAge  = 5 * time.Minute
	DefaultTimeout = 3 * time.Second
)

// maxBodyBytes caps the manifest body; a var so tests can lower it.
var maxBodyBytes int64 = 16 << 20

// Remote resolves the manifest of the backend the CLI talks to. It keeps one
// copy per BaseURL under CacheDir and asks the backend again only after MaxAge
// since the last check, sending the ETag so an unchanged manifest costs a 304.
// It never fails a command: an unreachable backend, a 4xx/5xx, or a manifest
// this binary cannot read falls back to the last copy, then to the embedded one.
type Remote struct {
	BaseURL  string
	Token    string
	CacheDir string
	HTTP     *http.Client
	Now      func() time.Time
	MaxAge   time.Duration
	Warn     io.Writer
}

// cacheMeta is what Remote remembers next to the copy: the ETag to send back
// and when the backend was last asked, successfully or not, so an offline
// backend is not retried on every invocation.
type cacheMeta struct {
	BaseURL   string    `json:"base_url"`
	ETag      string    `json:"etag"`
	CheckedAt time.Time `json:"checked_at"`
}

// Resolve returns the manifest to build the command tree from. With refresh
// false it never touches the network (offline commands, tests).
func (r *Remote) Resolve(ctx context.Context, refresh bool) (*Manifest, error) {
	cached, cachedSkipped, meta := r.readCache()
	if refresh && r.due(meta) {
		if m := r.fetch(ctx, cached, cachedSkipped, meta); m != nil {
			return m, nil
		}
	}
	if cached != nil {
		cached.Origin = OriginCache
		return cached, nil
	}
	m, err := Load()
	if err != nil {
		return nil, err
	}
	m.Origin = OriginEmbedded
	return m, nil
}

// due says whether to ask the backend now. Without a place to keep the copy a
// fetch would be thrown away on every command, so it never happens; a check
// stamped in the future (clock moved back) counts as due.
func (r *Remote) due(meta *cacheMeta) bool {
	if r.BaseURL == "" || r.Token == "" || r.dir() == "" {
		return false
	}
	if meta == nil {
		return true
	}
	elapsed := r.now().Sub(meta.CheckedAt)
	return elapsed < 0 || elapsed >= r.maxAge()
}

// fetch asks the backend for its manifest. It returns nil whenever the caller
// should fall back, after recording the check so the next one waits MaxAge.
// Every answer it uses, a 304 included, repeats the warning about commands this
// binary left out; reading the copy between checks stays silent.
func (r *Remote) fetch(ctx context.Context, cached *Manifest, cachedSkipped []Skipped, meta *cacheMeta) *Manifest {
	next := cacheMeta{BaseURL: r.BaseURL, CheckedAt: r.now()}
	if meta != nil {
		next.ETag = meta.ETag
	}
	defer func() { _ = r.writeMeta(next) }()

	resp, err := r.request(ctx, cached != nil, next.ETag)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotModified && cached != nil:
		r.warnNewer(cached, cachedSkipped)
		cached.Origin = OriginBackend
		return cached
	case resp.StatusCode != http.StatusOK:
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil || int64(len(body)) > maxBodyBytes {
		return nil
	}
	m, skipped, err := ParseLenient(body)
	if err == nil && len(m.Endpoints) == 0 {
		if len(skipped) == 0 {
			return nil
		}
		err = fmt.Errorf("none of its %d commands is readable", len(skipped))
	}
	if err != nil {
		r.warnf("warning: the backend manifest is not readable by this lk version (%v); using the previous copy. Run `lk update`.\n", err)
		return nil
	}
	if r.writeManifest(body) == nil {
		next.ETag = resp.Header.Get("ETag")
	}
	r.warnNewer(m, skipped)
	m.Origin = OriginBackend
	return m
}

// warnNewer says, once per fetch, what this binary left out of a manifest
// written for a newer lk. The other commands keep working.
func (r *Remote) warnNewer(m *Manifest, skipped []Skipped) {
	if len(skipped) > 0 {
		names := make([]string, len(skipped))
		for i, s := range skipped {
			names[i] = s.Command
			if names[i] == "" {
				names[i] = "(unnamed)"
			}
		}
		r.warnf("warning: %d backend command(s) need a newer lk and were left out: %s. Run `lk update`.\n",
			len(skipped), strings.Join(names, ", "))
	}
	if m.ManifestVersion > SupportedVersion {
		r.warnf("warning: the backend manifest is version %d and this lk knows up to %d; some commands may behave differently. Run `lk update`.\n",
			m.ManifestVersion, SupportedVersion)
	}
}

func (r *Remote) request(ctx context.Context, conditional bool, etag string) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(r.BaseURL, "/")+RemotePath, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.Token)
	if conditional && etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := r.httpClient().Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

// cancelOnClose releases the request's timeout context once the body is read,
// so the timeout also bounds reading a slow body.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelOnClose) Close() error {
	defer c.cancel()
	return c.ReadCloser.Close()
}

// readCache returns the stored copy, the commands of it this binary cannot
// build (left out, and reported only when the backend is asked again), and its
// metadata. A copy that does not decode, or keeps no command, is ignored, but
// its metadata still counts so a broken copy does not trigger a fetch on every
// command.
func (r *Remote) readCache() (*Manifest, []Skipped, *cacheMeta) {
	dir := r.dir()
	if dir == "" {
		return nil, nil, nil
	}
	var meta *cacheMeta
	if data, err := os.ReadFile(filepath.Join(dir, "meta.json")); err == nil {
		var m cacheMeta
		if json.Unmarshal(data, &m) == nil && m.BaseURL == r.BaseURL {
			meta = &m
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, nil, meta
	}
	m, skipped, err := ParseLenient(data)
	if err != nil || len(m.Endpoints) == 0 {
		return nil, nil, meta
	}
	return m, skipped, meta
}

func (r *Remote) writeManifest(body []byte) error {
	return r.writeFile("manifest.json", body)
}

func (r *Remote) writeMeta(meta cacheMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return r.writeFile("meta.json", data)
}

// writeFile replaces one cache file atomically, so a concurrent lk never reads
// a half-written manifest.
func (r *Remote) writeFile(name string, data []byte) error {
	dir := r.dir()
	if dir == "" {
		return errors.New("no cache directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(dir, fmt.Sprintf("%s.%d.tmp", name, os.Getpid()))
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, name))
}

// dir is the cache folder of this BaseURL: staging and production never share
// a copy, so a command that only exists in staging never shows up in production.
func (r *Remote) dir() string {
	if r.CacheDir == "" || r.BaseURL == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(r.BaseURL))
	return filepath.Join(r.CacheDir, hex.EncodeToString(sum[:8]))
}

func (r *Remote) warnf(format string, args ...any) {
	if r.Warn != nil {
		_, _ = fmt.Fprintf(r.Warn, format, args...)
	}
}

func (r *Remote) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Remote) maxAge() time.Duration {
	if r.MaxAge > 0 {
		return r.MaxAge
	}
	return DefaultMaxAge
}

func (r *Remote) httpClient() *http.Client {
	if r.HTTP != nil {
		return r.HTTP
	}
	return http.DefaultClient
}
