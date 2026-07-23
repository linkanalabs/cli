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

	"github.com/linkanalabs/cli/internal/manifest"
	"github.com/linkanalabs/cli/internal/output"
)

// runDynamic returns the RunE for a manifest endpoint: it resolves the
// credential, substitutes path params, routes changed flags into query or
// body (per the manifest's `in`), performs the request through the generic
// client and renders the raw JSON response.
func runDynamic(e *manifest.Endpoint) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		api, imp, _, err := resolveAPI()
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
		resp, err := api.Do(cmd.Context(), e.Method, path, query, payload)
		if err != nil {
			return err // ErrReadOnly already carries the `lk mode write` hint
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return unauthorizedErr(imp)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if len(resp.Body) > 0 {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), strings.TrimSpace(string(resp.Body)))
			}
			return fmt.Errorf("%s %s returned %d", e.Method, e.Path, resp.StatusCode)
		}
		if len(resp.Body) == 0 {
			return nil
		}
		return output.Render(cmd.OutOrStdout(), formatFlag(cmd), json.RawMessage(resp.Body))
	}
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
		if p.In == manifest.InQuery {
			if query == nil {
				query = url.Values{}
			}
			addQueryValue(query, p.Name, val)
		} else {
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
