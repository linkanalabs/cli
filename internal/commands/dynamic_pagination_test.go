package commands

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// pagedServer serves `total` gadgets in pages of `perPage`, recording the page
// values it was asked for. It mirrors the backend contract the capability
// describes: ?page= is 1-based and a page past the end is an empty array.
func pagedServer(t *testing.T, total, perPage int, pages *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		*pages = append(*pages, page)
		n := 1
		if page != "" {
			_, _ = fmt.Sscanf(page, "%d", &n)
		}
		start := (n - 1) * perPage
		var items []string
		for i := start; i < start+perPage && i < total; i++ {
			items = append(items, fmt.Sprintf(`{"id":"g_%d"}`, i))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[" + strings.Join(items, ",") + "]"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func pagedEnv(t *testing.T, srv *httptest.Server) {
	t.Helper()
	swapFixtureManifest(t)
	authEnv(t)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")
}

// The capability must produce the flags; a paged endpoint never declares them.
func TestPaginationFlagsComeFromTheCapability(t *testing.T) {
	swapFixtureManifest(t)
	root := newRootCmd()

	gadget := findCommand(root, "gadget", "list")
	if gadget == nil {
		t.Fatal("gadget list not registered")
	}
	if gadget.Flags().Lookup("page") == nil {
		t.Error("--page not registered from the pagination capability")
	}
	if gadget.Flags().Lookup("all") == nil {
		t.Error("--all not registered from the pagination capability")
	}

	// An endpoint without the capability must not gain --all (its own "page"
	// param is a plain query param, not paging).
	widget := findCommand(root, "widget", "list")
	if widget == nil {
		t.Fatal("widget list not registered")
	}
	if widget.Flags().Lookup("all") != nil {
		t.Error("--all leaked onto an endpoint without the pagination capability")
	}
}

func TestPaginationAllWalksEveryPageAndConcatenates(t *testing.T) {
	var pages []string
	srv := pagedServer(t, 5, 2, &pages) // 3 pages: 2, 2, 1
	pagedEnv(t, srv)

	var out, errOut bytes.Buffer
	if code := run([]string{"gadget", "list", "--all", "--format", "count"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	// count must be the real total, not one page's worth.
	if got := strings.TrimSpace(out.String()); got != "5" {
		t.Errorf("count = %q, want 5 (the real total)", got)
	}
	// Stops on the short page: no request for page 4.
	if want := []string{"1", "2", "3"}; strings.Join(pages, ",") != strings.Join(want, ",") {
		t.Errorf("pages requested = %v, want %v", pages, want)
	}
}

func TestPaginationAllStopsOnAnEmptyPage(t *testing.T) {
	var pages []string
	srv := pagedServer(t, 4, 2, &pages) // exactly 2 full pages, then empty
	pagedEnv(t, srv)

	var out, errOut bytes.Buffer
	if code := run([]string{"gadget", "list", "--all", "--format", "count"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if got := strings.TrimSpace(out.String()); got != "4" {
		t.Errorf("count = %q, want 4", got)
	}
	if want := []string{"1", "2", "3"}; strings.Join(pages, ",") != strings.Join(want, ",") {
		t.Errorf("pages requested = %v, want %v (page 3 confirms the end)", pages, want)
	}
}

func TestPaginationAllKeepsOtherQueryParams(t *testing.T) {
	var pages []string
	var gotQ []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages = append(pages, r.URL.Query().Get("page"))
		gotQ = append(gotQ, r.URL.Query().Get("q"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	pagedEnv(t, srv)

	var out, errOut bytes.Buffer
	if code := run([]string{"gadget", "list", "--all", "--q", "acme"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if len(gotQ) == 0 || gotQ[0] != "acme" {
		t.Errorf("q = %v, want the caller's filter carried into every page", gotQ)
	}
}

func TestPaginationPageAndAllAreMutuallyExclusive(t *testing.T) {
	var pages []string
	srv := pagedServer(t, 1, 2, &pages)
	pagedEnv(t, srv)

	var out, errOut bytes.Buffer
	code := run([]string{"gadget", "list", "--all", "--page", "2"}, &out, &errOut)
	if code == 0 {
		t.Fatal("exit = 0, want failure when --page and --all are combined")
	}
	if !strings.Contains(errOut.String(), "mutually exclusive") {
		t.Errorf("stderr = %q, want a mutually-exclusive error", errOut.String())
	}
	if len(pages) != 0 {
		t.Errorf("requests = %v, want none (fail before spending a request)", pages)
	}
}

func TestPaginationRejectsPageBelowOne(t *testing.T) {
	var pages []string
	srv := pagedServer(t, 1, 2, &pages)
	pagedEnv(t, srv)

	var out, errOut bytes.Buffer
	code := run([]string{"gadget", "list", "--page", "0"}, &out, &errOut)
	if code == 0 {
		t.Fatal("exit = 0, want failure for --page 0 (backend 500s on it)")
	}
	if !strings.Contains(errOut.String(), ">= 1") {
		t.Errorf("stderr = %q, want a >= 1 error", errOut.String())
	}
	if len(pages) != 0 {
		t.Errorf("requests = %v, want none", pages)
	}
}

func TestPaginationExplicitPageIsSentAsQuery(t *testing.T) {
	var pages []string
	srv := pagedServer(t, 10, 2, &pages)
	pagedEnv(t, srv)

	var out, errOut bytes.Buffer
	if code := run([]string{"gadget", "list", "--page", "3", "--format", "count"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if len(pages) != 1 || pages[0] != "3" {
		t.Errorf("pages requested = %v, want exactly [3]", pages)
	}
}

func TestPaginationWarnsOnAFullPage(t *testing.T) {
	var pages []string
	srv := pagedServer(t, 10, 2, &pages) // page 1 comes back full
	pagedEnv(t, srv)

	var out, errOut bytes.Buffer
	if code := run([]string{"gadget", "list", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "probably more") {
		t.Errorf("stderr = %q, want a warning that the page is full", errOut.String())
	}
	if !strings.Contains(errOut.String(), "--all") {
		t.Errorf("stderr = %q, want the warning to point at --all", errOut.String())
	}
	// The warning must not pollute stdout: it stays parseable data.
	if strings.Contains(out.String(), "probably more") {
		t.Errorf("stdout = %q, want data only", out.String())
	}
}

func TestPaginationDoesNotWarnOnAShortPage(t *testing.T) {
	var pages []string
	srv := pagedServer(t, 1, 2, &pages) // short page: that is the whole thing
	pagedEnv(t, srv)

	var out, errOut bytes.Buffer
	if code := run([]string{"gadget", "list", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if strings.Contains(errOut.String(), "probably more") {
		t.Errorf("stderr = %q, want no warning for a short page", errOut.String())
	}
}

func TestPaginationAllFailsWhenAPageIsNotAnArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"not":"an array"}`))
	}))
	t.Cleanup(srv.Close)
	pagedEnv(t, srv)

	var out, errOut bytes.Buffer
	code := run([]string{"gadget", "list", "--all"}, &out, &errOut)
	if code == 0 {
		t.Fatal("exit = 0, want failure when a page is not an array")
	}
	if !strings.Contains(errOut.String(), "JSON array") {
		t.Errorf("stderr = %q, want an explanation about the array contract", errOut.String())
	}
}

func TestPaginationAllPropagatesAnErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"nope"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"id":"g_0"},{"id":"g_1"}]`))
	}))
	t.Cleanup(srv.Close)
	pagedEnv(t, srv)

	var out, errOut bytes.Buffer
	code := run([]string{"gadget", "list", "--all"}, &out, &errOut)
	if code == 0 {
		t.Fatal("exit = 0, want the mid-walk error to surface")
	}
	if !strings.Contains(errOut.String(), "403") {
		t.Errorf("stderr = %q, want the 403 reported", errOut.String())
	}
}

func TestPaginationAllHandlesUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	pagedEnv(t, srv)

	var out, errOut bytes.Buffer
	code := run([]string{"gadget", "list", "--all"}, &out, &errOut)
	if code == 0 {
		t.Fatal("exit = 0, want failure on 401")
	}
	if !strings.Contains(errOut.String(), "lk auth login") {
		t.Errorf("stderr = %q, want the login hint", errOut.String())
	}
}
