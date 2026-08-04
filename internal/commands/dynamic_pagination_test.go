package commands

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// pagedServer mirrors the backend contract: one page in the body (a bare JSON
// array) and the page metadata in the pagy headers.
func pagedServer(t *testing.T, total, perPage int, seenPages *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.URL.Query().Get("page")
		if seenPages != nil {
			*seenPages = append(*seenPages, raw)
		}
		page := 1
		if raw != "" {
			page, _ = strconv.Atoi(raw)
		}
		pages := (total + perPage - 1) / perPage
		var items []string
		for i := (page - 1) * perPage; i < page*perPage && i < total; i++ {
			items = append(items, fmt.Sprintf(`{"id":"g_%d"}`, i))
		}
		w.Header().Set("total-count", strconv.Itoa(total))
		w.Header().Set("total-pages", strconv.Itoa(pages))
		w.Header().Set("current-page", strconv.Itoa(page))
		w.Header().Set("page-items", strconv.Itoa(perPage))
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

// The capability produces --page; a paged endpoint never declares it. And no
// walk-everything flag exists: the total comes from the header instead.
func TestPaginationFlagComesFromTheCapability(t *testing.T) {
	swapFixtureManifest(t)
	root := newRootCmd()

	gadget := findCommand(root, "gadget", "list")
	if gadget == nil {
		t.Fatal("gadget list not registered")
	}
	if gadget.Flags().Lookup("page") == nil {
		t.Error("--page not registered from the pagination capability")
	}
	if gadget.Flags().Lookup("all") != nil {
		t.Error("--all should not exist: the header total replaces walking every page")
	}

	widget := findCommand(root, "widget", "list")
	if widget == nil {
		t.Fatal("widget list not registered")
	}
	if widget.Flags().Lookup("all") != nil {
		t.Error("--all leaked onto an endpoint without the capability")
	}
}

// The point of the whole design: one request answers "how many are there".
func TestPaginationCountReportsTheCollectionTotalNotThePage(t *testing.T) {
	var pages []string
	srv := pagedServer(t, 47, 10, &pages)
	pagedEnv(t, srv)

	var out, errOut bytes.Buffer
	if code := run([]string{"gadget", "list", "--format", "count"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if got := strings.TrimSpace(out.String()); got != "47" {
		t.Errorf("count = %q, want 47 (the real total, not the 10 in this page)", got)
	}
	if len(pages) != 1 {
		t.Errorf("requests = %v, want exactly one", pages)
	}
}

// Asking for one page means the caller wants that page's size.
func TestPaginationCountWithExplicitPageCountsThatPage(t *testing.T) {
	srv := pagedServer(t, 47, 10, nil)
	pagedEnv(t, srv)

	var out, errOut bytes.Buffer
	if code := run([]string{"gadget", "list", "--page", "5", "--format", "count"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if got := strings.TrimSpace(out.String()); got != "7" {
		t.Errorf("count = %q, want 7 (the last page's size)", got)
	}
}

func TestPaginationReportsWhereThePageSits(t *testing.T) {
	srv := pagedServer(t, 47, 10, nil)
	pagedEnv(t, srv)

	var out, errOut bytes.Buffer
	if code := run([]string{"gadget", "list", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	for _, want := range []string{"page 1 of 5", "47 records in total", "--page 2"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr = %q, want it to contain %q", errOut.String(), want)
		}
	}
	// The report must not pollute stdout: it stays parseable data.
	if strings.Contains(out.String(), "records in total") {
		t.Errorf("stdout = %q, want data only", out.String())
	}
}

func TestPaginationOmitsTheNextHintOnTheLastPage(t *testing.T) {
	srv := pagedServer(t, 47, 10, nil)
	pagedEnv(t, srv)

	var out, errOut bytes.Buffer
	if code := run([]string{"gadget", "list", "--page", "5", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "page 5 of 5") {
		t.Errorf("stderr = %q, want the position reported", errOut.String())
	}
	if strings.Contains(errOut.String(), "next page") {
		t.Errorf("stderr = %q, want no next-page hint on the last page", errOut.String())
	}
}

func TestPaginationStaysQuietWhenEverythingFitsInOnePage(t *testing.T) {
	srv := pagedServer(t, 3, 10, nil)
	pagedEnv(t, srv)

	var out, errOut bytes.Buffer
	if code := run([]string{"gadget", "list", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr = %q, want silence when there is a single page", errOut.String())
	}
}

func TestPaginationExplicitPageIsSentAsQuery(t *testing.T) {
	var pages []string
	srv := pagedServer(t, 47, 10, &pages)
	pagedEnv(t, srv)

	var out, errOut bytes.Buffer
	if code := run([]string{"gadget", "list", "--page", "3", "--format", "json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if len(pages) != 1 || pages[0] != "3" {
		t.Errorf("pages requested = %v, want exactly [3]", pages)
	}
}

func TestPaginationRejectsPageBelowOne(t *testing.T) {
	var pages []string
	srv := pagedServer(t, 5, 10, &pages)
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
		t.Errorf("requests = %v, want none (fail before spending a request)", pages)
	}
}

// An endpoint that sends no pagination headers must behave exactly as before.
func TestPaginationDegradesWhenTheBackendSendsNoHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"id":"g_0"},{"id":"g_1"}]`))
	}))
	t.Cleanup(srv.Close)
	pagedEnv(t, srv)

	var out, errOut bytes.Buffer
	if code := run([]string{"gadget", "list", "--format", "count"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if got := strings.TrimSpace(out.String()); got != "2" {
		t.Errorf("count = %q, want 2 (falls back to counting the body)", got)
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr = %q, want no report without metadata", errOut.String())
	}
}

// An unpaged endpoint must not gain any of this behaviour.
func TestPaginationLeavesUnpagedEndpointsAlone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("total-count", "999") // even if a header shows up
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"id":"w_0"}]`))
	}))
	t.Cleanup(srv.Close)
	pagedEnv(t, srv)

	var out, errOut bytes.Buffer
	if code := run([]string{"widget", "list", "--format", "count"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if got := strings.TrimSpace(out.String()); got != "1" {
		t.Errorf("count = %q, want 1: an unpaged endpoint counts its body", got)
	}
}
