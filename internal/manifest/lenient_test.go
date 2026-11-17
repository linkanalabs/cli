package manifest

import (
	"context"
	"strings"
	"testing"
	"time"
)

const partlyNewerManifest = `{"manifest_version":1,"generated_at":"2026-11-17T00:00:00Z","source":"linkanalabs/linkana@newer","endpoints":[
	{"command":["thing","list"],"summary":"List things","method":"GET","path":"/things","params":[]},
	{"command":["thing","sync"],"summary":"Sync things","method":"POST","path":"/things/sync","body_root":"thing","params":[{"name":"since","type":"vector","in":"body"}]},
	{"command":[],"method":"GET","path":"/x"}]}`

func TestParseLenientDropsOnlyTheEndpointsItCannotBuild(t *testing.T) {
	m, skipped, err := ParseLenient([]byte(partlyNewerManifest))
	if err != nil {
		t.Fatalf("ParseLenient: %v", err)
	}
	if len(m.Endpoints) != 1 || m.Endpoints[0].CommandPath() != "thing list" {
		t.Errorf("kept %+v", m.Endpoints)
	}
	if len(skipped) != 2 || skipped[0].Command != "thing sync" || !strings.Contains(skipped[0].Reason, `unknown type "vector"`) || skipped[1].Command != "" {
		t.Errorf("skipped %+v", skipped)
	}
	if _, err := Parse([]byte(partlyNewerManifest)); err == nil {
		t.Errorf("Parse accepted the endpoint ParseLenient dropped")
	}
	if _, _, err := ParseLenient([]byte("{")); err == nil {
		t.Errorf("ParseLenient accepted broken JSON")
	}
}

func TestRemoteKeepsTheCommandsItUnderstandsAndWarnsAboutTheRest(t *testing.T) {
	b := newBackend(t)
	b.body = partlyNewerManifest
	c := &clock{t: time.Now()}
	r, warn := newRemote(t, b, c)

	m := resolve(t, r, true)

	if m.Origin != OriginBackend || len(m.Endpoints) != 1 || m.Endpoints[0].CommandPath() != "thing list" {
		t.Fatalf("origin %q endpoints %+v", m.Origin, m.Endpoints)
	}
	got := warn.String()
	if !strings.Contains(got, "2 backend command(s) need a newer lk and were left out: thing sync, (unnamed)") || !strings.Contains(got, "lk update") {
		t.Errorf("warning = %q", got)
	}

	warn.Reset()
	if m := resolve(t, r, false); m.Origin != OriginCache || len(m.Endpoints) != 1 {
		t.Errorf("cached copy: origin %q endpoints %d", m.Origin, len(m.Endpoints))
	}
	if warn.Len() != 0 {
		t.Errorf("reading the copy warned again: %q", warn.String())
	}
}

func TestRemoteWarnsAboutANewerManifestVersion(t *testing.T) {
	b := newBackend(t)
	b.body = strings.Replace(remoteManifest, `"manifest_version":1`, `"manifest_version":2`, 1)
	r, warn := newRemote(t, b, &clock{t: time.Now()})

	m, err := r.Resolve(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}

	if m.Origin != OriginBackend || len(m.Endpoints) != 1 {
		t.Errorf("origin %q endpoints %d", m.Origin, len(m.Endpoints))
	}
	if !strings.Contains(warn.String(), "manifest is version 2 and this lk knows up to 1") {
		t.Errorf("warning = %q", warn.String())
	}
}
