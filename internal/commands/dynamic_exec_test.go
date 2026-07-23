package commands

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestDynamicExecGetWithQuery(t *testing.T) {
	swapFixtureManifest(t)
	authEnv(t)
	var gotPath, gotAuth string
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"id":"w_1","name":"Widget One"}]`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	code := run([]string{
		"widget", "list", "--format", "json",
		"--q", "acme inc", "--page", "2", "--active",
		"--tags", "a", "--tags", "b",
		"--filter", `{"scope":"all"}`,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if gotPath != "/widgets.json" {
		t.Errorf("path = %q, want /widgets.json", gotPath)
	}
	if gotAuth != "Bearer lkn_abc_def" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotQuery.Get("q") != "acme inc" || gotQuery.Get("page") != "2" || gotQuery.Get("active") != "true" {
		t.Errorf("query = %v", gotQuery)
	}
	if tags := gotQuery["tags[]"]; len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Errorf("tags[] = %v", gotQuery["tags[]"])
	}
	if gotQuery.Get("filter") != `{"scope":"all"}` {
		t.Errorf("filter = %q", gotQuery.Get("filter"))
	}
	if !strings.Contains(out.String(), `"Widget One"`) {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestDynamicExecUnchangedFlagsSendNoQuery(t *testing.T) {
	swapFixtureManifest(t)
	authEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("unexpected query: %q", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	if code := run([]string{"widget", "list"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
}

func TestDynamicExecMultiplePathParamsEscaped(t *testing.T) {
	swapFixtureManifest(t)
	authEnv(t)
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.RequestURI
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"n_2","body":"hello"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	code := run([]string{"widget", "note", "show", "w 1", "n_2", "--format", "json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if gotURI != "/widgets/w%201/notes/n_2.json" {
		t.Errorf("request URI = %q", gotURI)
	}
	if !strings.Contains(out.String(), `"hello"`) {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestDynamicExecPostBodyWithRoot(t *testing.T) {
	swapFixtureManifest(t)
	authEnv(t)
	var gotMethod, gotPath, gotContentType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"w_9","name":"New Widget"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")
	enableWrite(t, srv.URL)

	var out, errOut bytes.Buffer
	code := run([]string{
		"widget", "create", "--format", "json",
		"--name", "New Widget", "--count", "3", "--enabled",
		"--price", "9.90", "--due_on", "2026-08-01",
		"--labels", "x", "--labels", "y",
		"--counts", "1", "--counts", "2",
		"--toggles", "true", "--toggles", "false",
		"--metadata", `{"origin":"cli"}`,
		"--items", `[{"sku":"a","qty":1}]`,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if gotMethod != http.MethodPost || gotPath != "/widgets.json" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q", gotContentType)
	}
	var body map[string]map[string]any
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("body not JSON: %v (%q)", err, gotBody)
	}
	w := body["widget"]
	if w == nil {
		t.Fatalf("body must be wrapped in body_root: %q", gotBody)
	}
	if w["name"] != "New Widget" || w["count"] != float64(3) || w["enabled"] != true {
		t.Errorf("scalars = %v", w)
	}
	if w["price"] != "9.90" || w["due_on"] != "2026-08-01" {
		t.Errorf("decimal/date must stay strings: %v", w)
	}
	if labels, _ := w["labels"].([]any); len(labels) != 2 || labels[0] != "x" {
		t.Errorf("labels = %v", w["labels"])
	}
	if counts, _ := w["counts"].([]any); len(counts) != 2 || counts[0] != float64(1) {
		t.Errorf("counts must be JSON numbers: %v", w["counts"])
	}
	if toggles, _ := w["toggles"].([]any); len(toggles) != 2 || toggles[0] != true || toggles[1] != false {
		t.Errorf("toggles must be JSON booleans: %v", w["toggles"])
	}
	if meta, _ := w["metadata"].(map[string]any); meta["origin"] != "cli" {
		t.Errorf("metadata = %v", w["metadata"])
	}
	items, _ := w["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v", w["items"])
	}
	if item, _ := items[0].(map[string]any); item["sku"] != "a" || item["qty"] != float64(1) {
		t.Errorf("items[0] = %v", items[0])
	}
	if !strings.Contains(out.String(), `"w_9"`) {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestDynamicExecWriteBlockedInReadMode(t *testing.T) {
	swapFixtureManifest(t)
	authEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("read-mode write must not reach the network")
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	code := run([]string{"widget", "create", "--name", "X"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "read mode") {
		t.Errorf("stderr should mention read mode: %q", errOut.String())
	}
}

func TestDynamicExecDelete204(t *testing.T) {
	swapFixtureManifest(t)
	authEnv(t)
	var gotMethod string
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotQuery = r.URL.Query()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")
	enableWrite(t, srv.URL)

	var out, errOut bytes.Buffer
	code := run([]string{"widget", "delete", "w_1", "--force"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if gotMethod != http.MethodDelete || gotQuery.Get("force") != "true" {
		t.Errorf("request = %s query=%v", gotMethod, gotQuery)
	}
	if out.String() != "" {
		t.Errorf("204 must produce no stdout, got %q", out.String())
	}
}

func TestDynamicExecInvalidJSONFlag(t *testing.T) {
	swapFixtureManifest(t)
	authEnv(t)
	t.Setenv("LK_TOKEN", "lkn_abc_def")
	enableWrite(t, "http://localhost:3000")

	var out, errOut bytes.Buffer
	code := run([]string{"widget", "create", "--name", "X", "--metadata", "{bad"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "--metadata") || !strings.Contains(errOut.String(), "valid JSON") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestDynamicExecJSONFlagWrongShape(t *testing.T) {
	cases := map[string]struct {
		args []string
		want string
	}{
		"object flag given an array":       {[]string{"widget", "create", "--name", "X", "--metadata", `[1,2]`}, "--metadata must be a JSON object"},
		"object flag given a scalar":       {[]string{"widget", "create", "--name", "X", "--metadata", `"str"`}, "--metadata must be a JSON object"},
		"array flag given an object":       {[]string{"widget", "create", "--name", "X", "--items", `{"a":1}`}, "--items must be a JSON array of objects"},
		"array flag with a scalar element": {[]string{"widget", "create", "--name", "X", "--items", `[{"a":1},2]`}, "--items: element 1 is not a JSON object"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			swapFixtureManifest(t)
			authEnv(t)
			t.Setenv("LK_TOKEN", "lkn_abc_def")
			enableWrite(t, "http://localhost:3000")

			var out, errOut bytes.Buffer
			code := run(tc.args, &out, &errOut)
			if code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			if !strings.Contains(errOut.String(), tc.want) {
				t.Errorf("stderr = %q, want it to contain %q", errOut.String(), tc.want)
			}
		})
	}
}

func TestDynamicExecInvalidIntegerArrayItem(t *testing.T) {
	swapFixtureManifest(t)
	authEnv(t)
	t.Setenv("LK_TOKEN", "lkn_abc_def")
	enableWrite(t, "http://localhost:3000")

	var out, errOut bytes.Buffer
	code := run([]string{"widget", "create", "--name", "X", "--counts", "abc"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "not an integer") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestDynamicExecInvalidBooleanArrayItem(t *testing.T) {
	swapFixtureManifest(t)
	authEnv(t)
	t.Setenv("LK_TOKEN", "lkn_abc_def")
	enableWrite(t, "http://localhost:3000")

	var out, errOut bytes.Buffer
	code := run([]string{"widget", "create", "--name", "X", "--toggles", "maybe"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "not a boolean") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestDynamicExecUnauthorizedHintsLogin(t *testing.T) {
	swapFixtureManifest(t)
	authEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_bad_token")

	var out, errOut bytes.Buffer
	code := run([]string{"widget", "list"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "lk auth login") {
		t.Errorf("stderr should hint re-login: %q", errOut.String())
	}
}

func TestDynamicExecNoTokenHintsLogin(t *testing.T) {
	swapFixtureManifest(t)
	authEnv(t)

	var out, errOut bytes.Buffer
	code := run([]string{"widget", "list"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "lk auth login") {
		t.Errorf("stderr should hint login: %q", errOut.String())
	}
}

func TestDynamicExecNon2xxWritesBodyToStderr(t *testing.T) {
	swapFixtureManifest(t)
	authEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"errors":{"content":["too long"]}}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")
	enableWrite(t, srv.URL)

	var out, errOut bytes.Buffer
	code := run([]string{"widget", "create", "--name", "X"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), `"too long"`) {
		t.Errorf("stderr should carry the response body: %q", errOut.String())
	}
	if !strings.Contains(errOut.String(), "422") {
		t.Errorf("stderr should mention the status: %q", errOut.String())
	}
	if out.String() != "" {
		t.Errorf("stdout must stay clean on failure, got %q", out.String())
	}
}

func TestDynamicExecTransportError(t *testing.T) {
	swapFixtureManifest(t)
	authEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead := srv.URL
	srv.Close()
	t.Setenv("LK_API_URL", dead)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	if code := run([]string{"widget", "list"}, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestDynamicExecInvalidJSONResponse(t *testing.T) {
	swapFixtureManifest(t)
	authEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	if code := run([]string{"widget", "list", "--format", "json"}, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestDynamicExecRequiredFlagEnforced(t *testing.T) {
	swapFixtureManifest(t)
	authEnv(t)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	code := run([]string{"widget", "create"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "name") {
		t.Errorf("stderr should mention the missing required flag: %q", errOut.String())
	}
}

func TestDynamicExecExactArgsEnforced(t *testing.T) {
	swapFixtureManifest(t)
	authEnv(t)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	if code := run([]string{"widget", "note", "show", "only-one"}, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}
