package commands

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These tests drive `supplier list` and `supplier show` through the REAL
// embedded manifest (no swapFixtureManifest), so they guard the vendored
// supplier endpoints end-to-end: a corrupted method/path in the embedded
// manifest, or a break in the generic executor that affected supplier
// specifically, would turn them red. The manual supplier command was removed
// in favor of these manifest-driven ones; without an execution-level test the
// only thing exercising them would be a human running the binary.

func TestSupplierListViaEmbeddedManifest(t *testing.T) {
	authEnv(t)
	var gotMethod, gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"id":"s_1","name":"Acme","identifier":"12345","legal_entity":"Acme Ltda","state":"active","tags":[{"id":"t_1","display_name":"Critical"}]}]`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	if code := run([]string{"supplier", "list", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if gotMethod != http.MethodGet || gotPath != "/srm/suppliers.json" {
		t.Errorf("request = %s %s, want GET /srm/suppliers.json", gotMethod, gotPath)
	}
	if gotAuth != "Bearer lkn_abc_def" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if !strings.Contains(out.String(), `"Acme"`) || !strings.Contains(out.String(), `"Critical"`) {
		t.Errorf("stdout should pass the supplier payload through: %q", out.String())
	}
}

func TestSupplierShowViaEmbeddedManifest(t *testing.T) {
	authEnv(t)
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"s_1","name":"Acme","identifier":"12345","legal_entity":"Acme Ltda","state":"active","tags":[]}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	if code := run([]string{"supplier", "show", "s_1", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	// :supplier_id is substituted into the path and the .json suffix kept.
	if gotMethod != http.MethodGet || gotPath != "/srm/suppliers/s_1/panel.json" {
		t.Errorf("request = %s %s, want GET /srm/suppliers/s_1/panel.json", gotMethod, gotPath)
	}
	if !strings.Contains(out.String(), `"s_1"`) {
		t.Errorf("stdout should pass the supplier panel through: %q", out.String())
	}
}

func TestSupplierShowRequiresID(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	// The :supplier_id path param maps to an ExactArgs(1) positional.
	if code := run([]string{"supplier", "show"}, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1 (missing positional id)", code)
	}
}
