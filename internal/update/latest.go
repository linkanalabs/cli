package update

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

// Where the published versions come from. Both are plain CDN reads on public
// repositories: no credentials, and deliberately not api.github.com, whose
// unauthenticated limit is 60 requests/hour *per IP* — a ceiling that a CLI
// driven by an agent would share with everyone behind the same corporate NAT.
var (
	// releasesLatestURL redirects to the newest release's tag page. Reading
	// the Location header costs one request against github.com (not the REST
	// API) and carries no rate-limit headers.
	releasesLatestURL = "https://github.com/linkanalabs/cli/releases/latest"

	// tapCaskURL is the cask GoReleaser commits on every release. It answers a
	// sharper question than the release list does: what `brew upgrade` would
	// actually install. The two diverge when a release ships but the tap commit
	// fails — the pitfall documented in CLAUDE.md.
	tapCaskURL = "https://raw.githubusercontent.com/linkanalabs/homebrew-tap/main/Casks/lk.rb"
)

// maxBodyBytes bounds every response this package reads.
const maxBodyBytes = 1 << 20

// caskVersion matches the `version "X"` stanza of a generated cask. Anchored at
// the start of a line so the interpolated `v#{version}` inside the download URLs
// cannot match.
//
// Compiled on first use, not at init: this package is imported by the command
// tree, so a package-level MustCompile would be paid on every lk startup for a
// pattern used at most once a day.
var caskVersion = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`(?m)^[ \t]*version[ \t]+"([^"]+)"`)
})

func parseCaskVersion(b []byte) string {
	m := caskVersion().FindSubmatch(b)
	if m == nil {
		return ""
	}
	return string(m[1])
}

// TapRemote returns the version the tap currently serves: what brew installs
// once its metadata is fresh.
func TapRemote(ctx context.Context, hc *http.Client) (string, error) {
	body, err := get(ctx, hc, tapCaskURL)
	if err != nil {
		return "", err
	}
	v := parseCaskVersion(body)
	if v == "" {
		return "", fmt.Errorf("no version stanza in the cask at %s", tapCaskURL)
	}
	return v, nil
}

// TapLocal returns the version in the cask file already on disk — brew's own
// metadata. It costs no network, and it is what decides whether a plain
// `brew upgrade` can do anything: when this trails TapRemote, brew has not
// fetched the new cask yet and upgrading would be a no-op.
func TapLocal(caskPath string) (string, error) {
	if caskPath == "" {
		return "", fmt.Errorf("no local cask path (install receipt did not name one)")
	}
	data, err := readFile(caskPath)
	if err != nil {
		return "", fmt.Errorf("reading the local cask %s: %w", caskPath, err)
	}
	v := parseCaskVersion(data)
	if v == "" {
		return "", fmt.Errorf("no version stanza in the local cask %s", caskPath)
	}
	return v, nil
}

// Release returns the newest published release tag, read from the redirect
// that /releases/latest issues.
func Release(ctx context.Context, hc *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, releasesLatestURL, nil)
	if err != nil {
		return "", fmt.Errorf("building the release request: %w", err)
	}

	// CheckRedirect is a client field, not a per-request one, so the client is
	// shallow-copied: callers share theirs with other probes (doctor reuses one
	// for backend reachability) and must not have it rewired underneath them.
	noFollow := *hc
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	resp, err := noFollow.Do(req)
	if err != nil {
		return "", fmt.Errorf("reaching %s: %w", releasesLatestURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("%s did not redirect (status %d)", releasesLatestURL, resp.StatusCode)
	}
	tag := tagFromLocation(loc)
	if tag == "" {
		return "", fmt.Errorf("no release tag in the redirect target %q", loc)
	}
	return tag, nil
}

// tagFromLocation pulls v0.8.1 out of .../releases/tag/v0.8.1.
func tagFromLocation(loc string) string {
	const marker = "/tag/"
	i := strings.LastIndex(loc, marker)
	if i < 0 {
		return ""
	}
	tag := loc[i+len(marker):]
	if j := strings.IndexAny(tag, "?#"); j >= 0 {
		tag = tag[:j]
	}
	return strings.Trim(tag, "/")
}

func get(ctx context.Context, hc *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building the request for %s: %w", url, err)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reaching %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", url, err)
	}
	return body, nil
}
