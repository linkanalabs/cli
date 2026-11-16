package commands

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/linkanalabs/cli/internal/auth"
	"github.com/linkanalabs/cli/internal/config"
	"github.com/linkanalabs/cli/internal/manifest"
	"github.com/linkanalabs/cli/internal/state"
)

// EnvNoRemoteManifest turns off asking the backend for its manifest: the CLI
// uses the last copy it has, or the embedded one.
const EnvNoRemoteManifest = "LK_NO_REMOTE_MANIFEST"

// manifestWarn receives the warning about a backend manifest this binary
// cannot read. A seam so tests can capture it.
var manifestWarn io.Writer = os.Stderr

// loadManifest is a seam so tests can substitute the manifest. refresh says
// whether this invocation may ask the backend for a newer one.
var loadManifest = resolveManifest

// offlineCommands never need the backend's manifest: they either do not call
// the backend at all or only touch configuration and credentials.
var offlineCommands = map[string]bool{
	"version":    true,
	"config":     true,
	"auth":       true,
	"doctor":     true,
	"update":     true,
	"completion": true,
}

// wantsFreshManifest reports whether the invocation may ask the backend for
// its manifest. The decision comes from the raw arguments because the command
// tree has to exist before cobra can tell which command will run.
func wantsFreshManifest(args []string) bool {
	if os.Getenv(EnvNoRemoteManifest) != "" {
		return false
	}
	for _, a := range args {
		if a == "--version" {
			return false
		}
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--format" {
			i++ // the global flag that takes a value: skip the value too
			continue
		}
		if len(a) > 0 && a[0] == '-' {
			continue
		}
		return !offlineCommands[a]
	}
	return true
}

// resolveManifest picks the manifest for the configured backend. It never
// fails because of the backend: without configuration or credentials it uses
// the last copy, then the embedded manifest.
func resolveManifest(refresh bool) (*manifest.Manifest, error) {
	r := &manifest.Remote{Warn: manifestWarn, Now: func() time.Time { return timeNow() }}
	if dir, err := state.Dir(); err == nil {
		r.CacheDir = filepath.Join(dir, "manifests")
	}
	if cfg, err := config.Load(); err == nil {
		r.BaseURL = cfg.BaseURL
		r.Token = manifestToken(cfg.BaseURL)
	}
	return r.Resolve(context.Background(), refresh)
}

// manifestToken is the credential the next command will use: the active
// impersonation when there is one and it has not expired, else the stored
// token. Any failure only means "do not ask the backend".
func manifestToken(baseURL string) string {
	if imp, err := auth.LoadImpersonation(baseURL); err == nil && imp != nil {
		if imp.Expired(timeNow()) {
			return ""
		}
		return imp.Token
	}
	token, _, err := authLoad(baseURL)
	if err != nil {
		return ""
	}
	return token
}
