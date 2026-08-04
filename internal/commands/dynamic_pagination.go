package commands

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/auth"
	"github.com/linkanalabs/cli/internal/client"
	"github.com/linkanalabs/cli/internal/manifest"
	"github.com/linkanalabs/cli/internal/output"
)

// maxPagesWalked bounds --all so a backend that never returns an empty page
// (a filter the CLI does not know about, a clamped page param) cannot spin
// forever. At the usual 10 per page this is 10k records — far past any real
// buyer, and the walk reports when it stops here.
const maxPagesWalked = 1000

// applyPagination puts an explicit --page onto the query, returning the query
// to use (collectParams leaves it nil when no query param changed, so paging
// may have to create it). It also rejects --page together with --all, which
// would silently ignore one of them.
func applyPagination(cmd *cobra.Command, e *manifest.Endpoint, query url.Values) (url.Values, error) {
	if e.Pagination == nil {
		return query, nil
	}
	flags := cmd.Flags()
	pageChanged := flags.Changed(e.Pagination.Param)
	allChanged := flags.Changed(manifest.AllFlagName)
	if pageChanged && allChanged {
		return nil, fmt.Errorf("--%s and --%s are mutually exclusive: --%s already walks every page",
			e.Pagination.Param, manifest.AllFlagName, manifest.AllFlagName)
	}
	if !pageChanged {
		return query, nil
	}
	page, err := flags.GetInt64(e.Pagination.Param)
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

// wantsAllPages reports whether the caller asked to walk every page.
func wantsAllPages(cmd *cobra.Command, e *manifest.Endpoint) bool {
	return e.Pagination != nil && cmd.Flags().Changed(manifest.AllFlagName)
}

// runAllPages walks the endpoint page by page and renders the concatenation as
// a single array, so `--format count` reports the real total instead of one
// page's worth. It stops at the first page that comes back empty (or short,
// which can only be the last one).
func runAllPages(
	cmd *cobra.Command,
	e *manifest.Endpoint,
	api client.API,
	imp *auth.Impersonation,
	path string,
	query url.Values,
	payload any,
) error {
	combined := make([]json.RawMessage, 0, e.Pagination.PerPage)
	for page := 1; ; page++ {
		if page > maxPagesWalked {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
				"warning: stopped after %d pages (%d records); the backend kept returning full pages\n",
				maxPagesWalked, len(combined))
			break
		}
		pageQuery := cloneQuery(query)
		pageQuery.Set(e.Pagination.Param, strconv.Itoa(page))

		resp, err := api.Do(cmd.Context(), e.Method, path, pageQuery, payload)
		if err != nil {
			return err
		}
		raw, err := successBody(cmd, e, resp, imp)
		if err != nil {
			return err
		}
		if raw == nil { // 2xx with no body: nothing more to walk
			break
		}
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return fmt.Errorf("--%s expects every page to be a JSON array, but page %d was not: %w",
				manifest.AllFlagName, page, err)
		}
		combined = append(combined, items...)
		// A short page can only be the last one; an empty page means we already
		// had everything. Either way, stop without spending another request.
		if len(items) < e.Pagination.PerPage {
			break
		}
	}
	encoded, err := json.Marshal(combined)
	if err != nil {
		return err
	}
	return output.Render(cmd.OutOrStdout(), formatFlag(cmd), json.RawMessage(encoded))
}

// warnIfPageIsFull tells the caller, on stderr, that a full page almost
// certainly means there is more — the JSON carries no pagination metadata, so
// without this an agent reads one page as if it were the whole collection.
// stdout stays pure data.
func warnIfPageIsFull(cmd *cobra.Command, e *manifest.Endpoint, raw []byte) {
	if e.Pagination == nil {
		return
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return // not an array: nothing to say about paging
	}
	if len(items) < e.Pagination.PerPage {
		return
	}
	page := int64(1)
	if cmd.Flags().Changed(e.Pagination.Param) {
		if v, err := cmd.Flags().GetInt64(e.Pagination.Param); err == nil {
			page = v
		}
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
		"warning: page %d is full (%d records) — there are probably more; use --%s for every page, or --%s %d for the next one\n",
		page, len(items), manifest.AllFlagName, e.Pagination.Param, page+1)
}

// cloneQuery copies the collected query so each page request can set its own
// page value without mutating the caller's.
func cloneQuery(src url.Values) url.Values {
	dst := make(url.Values, len(src)+1)
	for k, v := range src {
		dst[k] = append([]string(nil), v...)
	}
	return dst
}
