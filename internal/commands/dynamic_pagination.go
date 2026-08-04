package commands

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/manifest"
)

// Pagination metadata headers emitted by the backend (pagy's headers extra).
// The list bodies are bare JSON arrays, so the page metadata has to travel
// outside the body — these are what let the CLI report the real total from a
// single request instead of counting one page.
const (
	headerTotalCount = "total-count"
	headerTotalPages = "total-pages"
	headerPage       = "current-page"
)

// pageMeta is the pagination metadata of one response, when the backend sent
// it. Absent headers leave the fields at zero and every caller degrades to the
// page it received.
type pageMeta struct {
	total int
	pages int
	page  int
}

// readPageMeta pulls the pagination headers off a response. ok is false when
// the endpoint did not report any (an unpaged endpoint, or an older backend).
func readPageMeta(h http.Header) (pageMeta, bool) {
	total, err := strconv.Atoi(h.Get(headerTotalCount))
	if err != nil {
		return pageMeta{}, false
	}
	meta := pageMeta{total: total}
	meta.pages, _ = strconv.Atoi(h.Get(headerTotalPages))
	meta.page, _ = strconv.Atoi(h.Get(headerPage))
	return meta, true
}

// applyPagination puts an explicit --page onto the query, returning the query
// to use (collectParams leaves it nil when no query param changed, so paging
// may have to create it).
func applyPagination(cmd *cobra.Command, e *manifest.Endpoint, query url.Values) (url.Values, error) {
	if e.Pagination == nil || !cmd.Flags().Changed(e.Pagination.Param) {
		return query, nil
	}
	page, err := cmd.Flags().GetInt64(e.Pagination.Param)
	if err != nil {
		return nil, err
	}
	// The backend raises on page < 1 (Pagy::VariableError -> 500), so refuse it
	// here instead of spending a request to get a server error back.
	if page < 1 {
		return nil, fmt.Errorf("--%s must be >= 1, got %d", e.Pagination.Param, page)
	}
	if query == nil {
		query = url.Values{}
	}
	query.Set(e.Pagination.Param, strconv.FormatInt(page, 10))
	return query, nil
}

// reportPage tells the caller, on stderr, where this page sits in the whole
// collection — the body alone cannot say it. Without this an agent reads one
// page as if it were everything. stdout stays pure data.
func reportPage(cmd *cobra.Command, e *manifest.Endpoint, meta pageMeta, ok bool) {
	if e.Pagination == nil || !ok || meta.pages <= 1 {
		return
	}
	page := meta.page
	if page == 0 {
		page = 1
	}
	next := ""
	if page < meta.pages {
		next = fmt.Sprintf("; next page: --%s %d", e.Pagination.Param, page+1)
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
		"page %d of %d — %d records in total%s\n",
		page, meta.pages, meta.total, next)
}

// countFromMeta renders --format count as the collection's real total, taken
// from the header, instead of the size of the page that happened to arrive.
// That is what makes "how many are there?" a single request.
func countFromMeta(cmd *cobra.Command, meta pageMeta) error {
	_, err := fmt.Fprintln(cmd.OutOrStdout(), meta.total)
	return err
}

// countsWholeCollection reports whether --format count should answer with the
// header total. Asking for a specific page means the caller wants that page's
// size, not the collection's.
func countsWholeCollection(cmd *cobra.Command, e *manifest.Endpoint, ok bool) bool {
	return ok && e.Pagination != nil && !cmd.Flags().Changed(e.Pagination.Param)
}
