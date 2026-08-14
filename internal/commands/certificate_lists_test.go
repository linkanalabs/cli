package commands

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/linkanalabs/cli/internal/manifest"
)

// fastImportPoll shortens the progress interval so a test that exercises more
// than one poll does not wait on the real 10s.
func fastImportPoll(t *testing.T) {
	t.Helper()
	previous := importPollInterval
	importPollInterval = time.Millisecond
	t.Cleanup(func() { importPollInterval = previous })
}

type recordedRequest struct {
	path  string
	query string
	body  map[string]any
}

// importServer answers the chunk POSTs and then the progress GETs, in the shape
// the backend uses, recording every request it receives.
func importServer(t *testing.T, statuses []string) (*httptest.Server, *[]recordedRequest) {
	t.Helper()
	var requests []recordedRequest
	poll := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record := recordedRequest{path: r.URL.Path, query: r.URL.RawQuery}
		if r.Method == http.MethodPost {
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &record.body); err != nil {
				t.Errorf("body = %q, not JSON: %v", raw, err)
			}
		}
		requests = append(requests, record)

		status := "pending"
		if r.Method == http.MethodGet {
			status = statuses[min(poll, len(statuses)-1)]
			poll++
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"identifier":"lk-x","filename":"lista.csv","status":"` + status +
			`","total_count":2,"imported_count":2,"failed_count":0,"duplicated_count":0}`))
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func writeCSV(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lista.csv")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing csv: %v", err)
	}
	return path
}

func TestCertificateListImportChunksAndFollows(t *testing.T) {
	authEnv(t)
	fastImportPoll(t)
	server, requests := importServer(t, []string{"processing", "completed"})
	t.Setenv("LK_API_URL", server.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")
	csvPath := writeCSV(t, "CNPJ,Nome\n52.710.793/0001-36,ACME\n,Pessoa Fisica\n")

	var out, errOut bytes.Buffer
	code := run([]string{
		"settings", "certificate", "restriction-list", "import",
		"--id", "cert_1", "--file", csvPath, "--chunk-size", "1", "--format", "json",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}

	posts := 0
	var identifiers []string
	for _, request := range *requests {
		if request.path != "/srm_settings/certificates_internal_restriction_lists_csv_imports.json" {
			continue
		}
		posts++
		if request.query != "id=cert_1" {
			t.Errorf("query = %q, want id=cert_1", request.query)
		}
		payload, ok := request.body["certificates_internal_restriction_lists_csv_import"].(map[string]any)
		if !ok {
			t.Fatalf("body = %v, want the certificates_internal_restriction_lists_csv_import root", request.body)
		}
		identifiers = append(identifiers, payload["identifier"].(string))
		if payload["total_count"] != float64(2) {
			t.Errorf("total_count = %v, want 2 (the whole file, not the chunk)", payload["total_count"])
		}
		if payload["filename"] != csvPath {
			t.Errorf("filename = %v, want %q", payload["filename"], csvPath)
		}
		if rows, _ := payload["data"].([]any); len(rows) != 1 {
			t.Errorf("data = %v, want 1 row per chunk", payload["data"])
		}
	}
	if posts != 2 {
		t.Fatalf("posts = %d, want 2 chunks", posts)
	}
	if identifiers[0] != identifiers[1] {
		t.Errorf("identifiers = %v, want the same value on every chunk", identifiers)
	}
	if !strings.HasPrefix(identifiers[0], "lk-") {
		t.Errorf("identifier = %q, want the lk- prefix", identifiers[0])
	}

	polls := 0
	for _, request := range *requests {
		if request.path == "/srm_settings/certificates/cert_1/csv_imports/"+identifiers[0]+".json" {
			polls++
		}
	}
	if polls != 2 {
		t.Errorf("polls = %d, want 2 (processing then completed)", polls)
	}
	if !strings.Contains(out.String(), `"status": "completed"`) {
		t.Errorf("stdout = %q, want the final status", out.String())
	}
}

func TestCertificateListImportMapsRowValues(t *testing.T) {
	authEnv(t)
	fastImportPoll(t)
	server, requests := importServer(t, []string{"completed"})
	t.Setenv("LK_API_URL", server.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")
	csvPath := writeCSV(t, "\ufeffNome,CPF,CNPJ,Observacao\nACME, 111.222.333-44 ,,ignorado\n")

	var out, errOut bytes.Buffer
	if code := run([]string{
		"settings", "certificate", "restriction-list", "import",
		"--id", "cert_1", "--file", csvPath, "--format", "json",
	}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}

	payload := (*requests)[0].body["certificates_internal_restriction_lists_csv_import"].(map[string]any)
	rows := payload["data"].([]any)
	values := rows[0].(map[string]any)["values"].(map[string]any)
	if values["nome"] != "ACME" || values["cpf"] != "111.222.333-44" {
		t.Errorf("values = %v, want nome and a trimmed cpf", values)
	}
	if _, present := values["cnpj"]; present {
		t.Errorf("values = %v, want no cnpj key for an empty cell", values)
	}
	if _, present := values["observacao"]; present {
		t.Errorf("values = %v, want unknown columns dropped", values)
	}
	if rows[0].(map[string]any)["index"] != float64(0) {
		t.Errorf("index = %v, want 0", rows[0].(map[string]any)["index"])
	}
}

func TestCertificateListImportFromStdin(t *testing.T) {
	authEnv(t)
	fastImportPoll(t)
	server, requests := importServer(t, []string{"completed"})
	t.Setenv("LK_API_URL", server.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	stdin := strings.NewReader("cnae\n6201-5/00\n")
	code, _ := runWith(stdin, []string{
		"settings", "certificate", "cnae-list", "import",
		"--id", "cert_2", "--stdin", "--format", "json",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}

	first := (*requests)[0]
	if first.path != "/srm_settings/certificates_cnae_lists_csv_imports.json" {
		t.Errorf("path = %q, want the cnae list endpoint", first.path)
	}
	payload, ok := first.body["certificates_cnae_lists_csv_import"].(map[string]any)
	if !ok {
		t.Fatalf("body = %v, want the certificates_cnae_lists_csv_import root", first.body)
	}
	if payload["filename"] != "stdin.csv" {
		t.Errorf("filename = %v, want stdin.csv", payload["filename"])
	}
	values := payload["data"].([]any)[0].(map[string]any)["values"].(map[string]any)
	if values["cnae"] != "6201-5/00" {
		t.Errorf("values = %v, want the cnae", values)
	}
}

func TestCertificateListImportNoWaitSkipsPolling(t *testing.T) {
	authEnv(t)
	server, requests := importServer(t, []string{"pending"})
	t.Setenv("LK_API_URL", server.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")
	csvPath := writeCSV(t, "cnpj\n52.710.793/0001-36\n")

	var out, errOut bytes.Buffer
	if code := run([]string{
		"settings", "certificate", "restriction-list", "import",
		"--id", "cert_1", "--file", csvPath, "--no-wait", "--format", "json",
	}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if len(*requests) != 1 {
		t.Errorf("requests = %d, want only the chunk post", len(*requests))
	}
}

// TestCertificateListImportStopsOnDeleted covers the `clear`-while-importing
// case: without `deleted` in the terminal set the poll would never return.
func TestCertificateListImportStopsOnDeleted(t *testing.T) {
	authEnv(t)
	fastImportPoll(t)
	server, _ := importServer(t, []string{"deleted"})
	t.Setenv("LK_API_URL", server.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")
	csvPath := writeCSV(t, "cnpj\n52.710.793/0001-36\n")

	var out, errOut bytes.Buffer
	if code := run([]string{
		"settings", "certificate", "restriction-list", "import",
		"--id", "cert_1", "--file", csvPath, "--format", "json",
	}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"status": "deleted"`) {
		t.Errorf("stdout = %q, want the deleted status", out.String())
	}
}

// TestCertificateListImportRejectsErrorStatus covers the contract of
// client.Do: it reports transport errors only, so a 422 arrives as a normal
// response. Without the status-code check the Rails error body would decode as
// an import with an empty status and the poll would never end.
func TestCertificateListImportRejectsErrorStatus(t *testing.T) {
	authEnv(t)
	fastImportPoll(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"errors":["Identifier já está em uso"]}`))
	}))
	t.Cleanup(server.Close)
	t.Setenv("LK_API_URL", server.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")
	csvPath := writeCSV(t, "cnpj\n52.710.793/0001-36\n")

	var out, errOut bytes.Buffer
	code := run([]string{
		"settings", "certificate", "restriction-list", "import",
		"--id", "cert_1", "--file", csvPath, "--format", "json",
	}, &out, &errOut)
	if code == 0 {
		t.Fatalf("exit = 0, want a failure; stdout = %q", out.String())
	}
	if !strings.Contains(errOut.String(), "returned 422") {
		t.Errorf("stderr = %q, want it to report the 422", errOut.String())
	}
	if !strings.Contains(errOut.String(), "Identifier já está em uso") {
		t.Errorf("stderr = %q, want the backend error body", errOut.String())
	}
}

func TestCertificateListImportUnauthorized(t *testing.T) {
	authEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	t.Setenv("LK_API_URL", server.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")
	csvPath := writeCSV(t, "cnpj\n52.710.793/0001-36\n")

	var out, errOut bytes.Buffer
	if code := run([]string{
		"settings", "certificate", "restriction-list", "import",
		"--id", "cert_1", "--file", csvPath,
	}, &out, &errOut); code == 0 {
		t.Fatalf("exit = 0, want a failure; stdout = %q", out.String())
	}
	if !strings.Contains(errOut.String(), "lk auth login") {
		t.Errorf("stderr = %q, want the re-authentication hint", errOut.String())
	}
}

// TestCertificateListImportGivesUpAtTimeout keeps a never-finishing import from
// blocking forever: the command returns the last state it saw and points at the
// command that follows the rest.
func TestCertificateListImportGivesUpAtTimeout(t *testing.T) {
	authEnv(t)
	fastImportPoll(t)
	server, _ := importServer(t, []string{"processing"})
	t.Setenv("LK_API_URL", server.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")
	csvPath := writeCSV(t, "cnpj\n52.710.793/0001-36\n")

	var out, errOut bytes.Buffer
	if code := run([]string{
		"settings", "certificate", "restriction-list", "import",
		"--id", "cert_1", "--file", csvPath, "--timeout", "1ns", "--format", "json",
	}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "settings certificate import show") {
		t.Errorf("stderr = %q, want it to point at the progress command", errOut.String())
	}
	if !strings.Contains(out.String(), `"status": "processing"`) {
		t.Errorf("stdout = %q, want the last status it saw", out.String())
	}
}

// TestCertificateListImportRejectsStatuslessBody guards the poll against a 2xx
// whose body is not an import: an empty status is not a terminal state, so
// looping on it would only end at the timeout.
func TestCertificateListImportRejectsStatuslessBody(t *testing.T) {
	authEnv(t)
	fastImportPoll(t)
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodPost {
			posts++
			_, _ = w.Write([]byte(`{"identifier":"lk-x","status":"pending"}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	t.Setenv("LK_API_URL", server.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")
	csvPath := writeCSV(t, "cnpj\n52.710.793/0001-36\n")

	var out, errOut bytes.Buffer
	if code := run([]string{
		"settings", "certificate", "restriction-list", "import",
		"--id", "cert_1", "--file", csvPath,
	}, &out, &errOut); code == 0 {
		t.Fatalf("exit = 0, want a failure; stdout = %q", out.String())
	}
	if !strings.Contains(errOut.String(), "não trouxe status") {
		t.Errorf("stderr = %q, want the missing-status error", errOut.String())
	}
}

func TestCertificateListImportSourceAndCSVErrors(t *testing.T) {
	csvPath := writeCSV(t, "cnpj\n52.710.793/0001-36\n")
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no source", []string{"--id", "cert_1"}, "informe --file"},
		{"both sources", []string{"--id", "cert_1", "--file", csvPath, "--stdin"}, "não os dois"},
		{"missing file", []string{"--id", "cert_1", "--file", "/nao/existe.csv"}, "abrindo /nao/existe.csv"},
		{"bad chunk size", []string{"--id", "cert_1", "--file", csvPath, "--chunk-size", "0"}, "maior que zero"},
		{"unknown header", []string{"--id", "cert_1", "--file", writeCSV(t, "documento\n123\n")}, "nenhuma coluna reconhecida"},
		{"header only", []string{"--id", "cert_1", "--file", writeCSV(t, "cnpj\n")}, "nenhuma linha com dados"},
		{"missing id", []string{"--file", csvPath}, `required flag(s) "id" not set`},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			authEnv(t)
			var out, errOut bytes.Buffer
			args := append([]string{"settings", "certificate", "restriction-list", "import"}, testCase.args...)
			if code := run(args, &out, &errOut); code == 0 {
				t.Fatalf("exit = 0, want a failure; stdout = %q", out.String())
			}
			if !strings.Contains(errOut.String(), testCase.want) {
				t.Errorf("stderr = %q, want it to mention %q", errOut.String(), testCase.want)
			}
		})
	}
}

func TestSurfaceIncludesCertificateListImports(t *testing.T) {
	got := surface(newRootCmd())
	for _, want := range []string{
		"lk settings certificate restriction-list import",
		"lk settings certificate cnae-list import",
	} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("surface missing %q:\n%s", want, got)
		}
	}
}

// TestCertificateListGroupsMergeWithDynamic locks the coexistence the manual
// import depends on: the manifest hangs `import-chunk` and `clear` under the
// same `settings certificate <list>` groups this file creates by hand, so the
// dynamic registration has to reuse them instead of backing off.
func TestCertificateListGroupsMergeWithDynamic(t *testing.T) {
	m, err := manifest.Parse([]byte(`{
	  "version": 1,
	  "endpoints": [
	    {
	      "command": ["settings", "certificate", "restriction-list", "import-chunk"],
	      "method": "POST",
	      "path": "/srm_settings/certificates_internal_restriction_lists_csv_imports",
	      "summary": "Send one chunk",
	      "description": "Raw chunk endpoint.",
	      "response": "200 with the import",
	      "body_root": "certificates_internal_restriction_lists_csv_import"
	    },
	    {
	      "command": ["settings", "certificate", "restriction-list", "clear"],
	      "method": "DELETE",
	      "path": "/srm_settings/certificates_internal_restriction_lists_csv_imports",
	      "summary": "Wipe the lists",
	      "description": "Removes every row.",
	      "response": "200 with the certificate"
	    }
	  ]
	}`))
	if err != nil {
		t.Fatalf("parsing manifest: %v", err)
	}
	swapManifest(t, m, nil)

	root := newRootCmd()
	for _, path := range [][]string{
		{"settings", "certificate", "restriction-list", "import"},
		{"settings", "certificate", "restriction-list", "import-chunk"},
		{"settings", "certificate", "restriction-list", "clear"},
	} {
		if findCommand(root, path...) == nil {
			t.Errorf("missing command %v", path)
		}
	}
	if groups := findCommand(root, "settings", "certificate"); len(groups.Commands()) != 2 {
		t.Errorf("certificate subcommands = %d, want restriction-list and cnae-list without a duplicate",
			len(groups.Commands()))
	}
}

func TestNewImportIdentifierIsUnique(t *testing.T) {
	first, err := newImportIdentifier()
	if err != nil {
		t.Fatalf("newImportIdentifier: %v", err)
	}
	second, err := newImportIdentifier()
	if err != nil {
		t.Fatalf("newImportIdentifier: %v", err)
	}
	if first == second {
		t.Errorf("identifier = %q twice, want a new value per import", first)
	}
}
