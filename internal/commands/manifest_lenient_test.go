package commands

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/linkanalabs/cli/internal/manifest"
)

const newerBackendManifest = `{"manifest_version":1,"generated_at":"2026-11-17T00:00:00Z","source":"linkanalabs/linkana@newer","endpoints":[
	{"command":["gadget","list"],"summary":"List the buyer's gadgets","method":"GET","path":"/srm/gadgets","params":[],"response":"200"},
	{"command":["gadget","sync"],"summary":"Sync gadgets","method":"POST","path":"/srm/gadgets/sync","body_root":"gadget","params":[{"name":"since","type":"vector","in":"body"}],"response":"200"}]}`

func TestACommandThisLkCannotBuildDoesNotHideTheOthers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == manifest.RemotePath {
			_, _ = w.Write([]byte(newerBackendManifest))
			return
		}
		_, _ = w.Write([]byte(`[{"id":"g1"}]`))
	}))
	t.Cleanup(server.Close)
	useRemoteManifest(t, server.URL)
	var warn bytes.Buffer
	prev := manifestWarn
	manifestWarn = &warn
	t.Cleanup(func() { manifestWarn = prev })

	var out, errOut bytes.Buffer
	if code := run([]string{"gadget", "list", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"g1"`) {
		t.Errorf("stdout = %q", out.String())
	}
	if !strings.Contains(warn.String(), "left out: gadget sync") {
		t.Errorf("warning = %q", warn.String())
	}

	firstWarning := warn.String()
	out.Reset()
	if code := run([]string{"gadget", "--help"}, &out, &errOut); code != 0 {
		t.Fatalf("help exit %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "list") || strings.Contains(out.String(), "sync") {
		t.Errorf("help = %q", out.String())
	}
	if warn.String() != firstWarning {
		t.Errorf("the warning repeated on a cache read: %q", warn.String())
	}
}
