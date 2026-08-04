package commands

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/linkanalabs/cli/internal/auth"
	"github.com/linkanalabs/cli/internal/client"
	"github.com/linkanalabs/cli/internal/manifest"
	"github.com/linkanalabs/cli/internal/output"
)

// runDynamic returns the RunE for a manifest endpoint: it resolves the
// credential, substitutes path params, routes changed flags into query or
// body (per the manifest's `in`), performs the request through the generic
// client and renders the raw JSON response.
func runDynamic(e *manifest.Endpoint) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		api, imp, err := resolveAPI()
		if err != nil {
			return err
		}
		path := substitutePathParams(e, args)
		query, body, err := collectParams(cmd, e)
		if err != nil {
			return err
		}
		var payload any
		if body != nil {
			payload = body
			if e.BodyRoot != "" {
				payload = map[string]any{e.BodyRoot: body}
			}
		}
		query, err = applyPagination(cmd, e, query)
		if err != nil {
			return err
		}
		resp, err := api.Do(cmd.Context(), e.Method, path, query, payload)
		if err != nil {
			return err
		}
		raw, err := successBody(cmd, e, resp, imp)
		if err != nil || raw == nil {
			return err
		}
		meta, hasMeta := readPageMeta(resp.Header)
		// On a paged endpoint, `count` means the size of the collection, not of
		// the page that happened to arrive — the header carries the real total.
		if formatFlag(cmd) == output.FormatCount && countsWholeCollection(cmd, e, hasMeta) {
			return countFromMeta(cmd, meta)
		}
		reportPage(cmd, e, meta, hasMeta)
		return output.Render(cmd.OutOrStdout(), formatFlag(cmd), json.RawMessage(raw))
	}
}

// successBody turns a response into its raw 2xx body, or an error. A nil body
// with a nil error means "2xx carrying no record": the counting projection
// still owes its integer — a caller doing `n=$(lk ... --format count)` must
// never get an empty string — while every other format stays silent.
func successBody(cmd *cobra.Command, e *manifest.Endpoint, resp *client.Response, imp *auth.Impersonation) ([]byte, error) {
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, unauthorizedErr(imp)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if len(resp.Body) > 0 {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), strings.TrimSpace(string(resp.Body)))
		}
		return nil, fmt.Errorf("%s %s returned %d", e.Method, e.Path, resp.StatusCode)
	}
	if len(resp.Body) == 0 {
		if formatFlag(cmd) == output.FormatCount {
			return nil, output.Render(cmd.OutOrStdout(), output.FormatCount, nil)
		}
		return nil, nil
	}
	return resp.Body, nil
}

// substitutePathParams replaces each "/:param" segment with the matching
// positional argument, path-escaped. Arguments follow the path order.
func substitutePathParams(e *manifest.Endpoint, args []string) string {
	values := make(map[string]string, len(e.PathParams))
	for i, pp := range e.PathParams {
		values[":"+pp] = url.PathEscape(args[i])
	}
	segments := strings.Split(e.Path, "/")
	for i, seg := range segments {
		if v, ok := values[seg]; ok {
			segments[i] = v
		}
	}
	return strings.Join(segments, "/")
}

// collectParams turns the changed flags into query values and body fields,
// honoring each param's declared location.
func collectParams(cmd *cobra.Command, e *manifest.Endpoint) (url.Values, map[string]any, error) {
	var query url.Values
	var body map[string]any
	for i := range e.Params {
		p := &e.Params[i]
		if !cmd.Flags().Changed(p.Name) {
			continue
		}
		val, err := dynamicFlagValue(cmd.Flags(), p)
		if err != nil {
			return nil, nil, err
		}
		switch {
		case p.In == manifest.InQuery:
			if query == nil {
				query = url.Values{}
			}
			addQueryValue(query, p.Name, val)
		case p.Spread:
			// spread: the param's own value becomes the request body itself,
			// instead of nesting one level deeper under p.Name. The manifest
			// validates spread => type object, so dynamicFlagValue always
			// resolves this through jsonFlagValue(..., "object"), which
			// guarantees a map[string]any.
			body = val.(map[string]any)
		default:
			if body == nil {
				body = map[string]any{}
			}
			body[p.Name] = val
		}
	}
	return query, body, nil
}

// dynamicFlagValue extracts the typed value of a changed flag.
func dynamicFlagValue(flags *pflag.FlagSet, p *manifest.Param) (any, error) {
	switch p.Type {
	case manifest.TypeInteger:
		v, err := flags.GetInt64(p.Name)
		return v, err
	case manifest.TypeBoolean:
		v, err := flags.GetBool(p.Name)
		return v, err
	case manifest.TypeObject:
		return jsonFlagValue(flags, p.Name, "object")
	case manifest.TypeArray:
		if p.Item == manifest.TypeObject {
			return jsonFlagValue(flags, p.Name, "array")
		}
		items, err := flags.GetStringArray(p.Name)
		if err != nil {
			return nil, err
		}
		return convertArrayItems(p, items)
	default: // string, date, datetime, decimal
		v, err := flags.GetString(p.Name)
		return v, err
	}
}

// jsonFlagValue parses a JSON-string flag into a generic value.
func jsonFlagValue(flags *pflag.FlagSet, name, kind string) (any, error) {
	raw, err := flags.GetString(name)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, fmt.Errorf("--%s must be valid JSON (%s): %w", name, kind, err)
	}
	switch kind {
	case "object":
		if _, ok := v.(map[string]any); !ok {
			return nil, fmt.Errorf("--%s must be a JSON object", name)
		}
	case "array":
		items, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("--%s must be a JSON array of objects", name)
		}
		for i, item := range items {
			if _, ok := item.(map[string]any); !ok {
				return nil, fmt.Errorf("--%s: element %d is not a JSON object", name, i)
			}
		}
	}
	return v, nil
}

// convertArrayItems converts repeated string flag values into typed items per
// the manifest's item type, so the JSON body carries proper scalars.
func convertArrayItems(p *manifest.Param, items []string) ([]any, error) {
	out := make([]any, len(items))
	for i, s := range items {
		switch p.Item {
		case manifest.TypeInteger:
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("--%s: item %q is not an integer", p.Name, s)
			}
			out[i] = n
		case manifest.TypeBoolean:
			b, err := strconv.ParseBool(s)
			if err != nil {
				return nil, fmt.Errorf("--%s: item %q is not a boolean", p.Name, s)
			}
			out[i] = b
		default: // string, date, datetime, decimal
			out[i] = s
		}
	}
	return out, nil
}

// addQueryValue encodes a typed value into the query string. Arrays use the
// Rails convention of a repeated "name[]" key.
func addQueryValue(q url.Values, name string, val any) {
	if items, ok := val.([]any); ok {
		for _, item := range items {
			q.Add(name+"[]", queryScalar(item))
		}
		return
	}
	q.Set(name, queryScalar(val))
}

// queryScalar renders one value as a query-string scalar. JSON-derived values
// (objects/arrays from JSON flags) are re-serialized compactly.
func queryScalar(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case int64:
		return strconv.FormatInt(t, 10)
	case bool:
		return strconv.FormatBool(t)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}
