package commands

import (
	"bytes"
	"crypto/md5" //nolint:gosec // Active Storage direct upload contract.
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// uploadServer answers the direct_uploads#create POST with a signed_id and a
// storage URL pointing back at itself, then accepts the byte PUT — recording
// both requests so tests can assert on the whole dance.
func uploadServer(t *testing.T) (*httptest.Server, *[]recordedRequest, *[]byte) {
	t.Helper()
	var requests []recordedRequest
	var putBody []byte
	server := httptest.NewServer(http.HandlerFunc(nil))
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record := recordedRequest{path: r.URL.Path, query: r.URL.RawQuery}
		switch r.Method {
		case http.MethodPost:
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &record.body); err != nil {
				t.Errorf("body = %q, not JSON: %v", raw, err)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"signed_id":"sgid-123","filename":"logo.png","byte_size":16,` +
				`"content_type":"image/png","direct_upload":{"url":"` + server.URL + `/storage-put",` +
				`"headers":{"Content-Type":"image/png"}}}`))
		case http.MethodPut:
			putBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusNoContent)
		}
		requests = append(requests, record)
	})
	t.Cleanup(server.Close)
	return server, &requests, &putBody
}

func writeUploadFile(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("writing file: %v", err)
	}
	return path
}

func TestUploadFromFilePostsMetadataPutsBytesAndPrintsSignedID(t *testing.T) {
	authEnv(t)
	server, requests, putBody := uploadServer(t)
	t.Setenv("LK_API_URL", server.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	content := []byte("\x89PNG\r\n\x1a\nfakepng.")
	path := writeUploadFile(t, "logo.png", content)

	var out, errOut bytes.Buffer
	code := run([]string{"upload", "--file", path, "--format", "json"}, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut.String())
	}
	if len(*requests) != 2 {
		t.Fatalf("requests = %d, want POST + PUT", len(*requests))
	}

	post := (*requests)[0]
	if !strings.HasPrefix(post.path, "/rails/active_storage/direct_uploads") {
		t.Errorf("POST path = %s", post.path)
	}
	blob := post.body["blob"].(map[string]any)
	if blob["filename"] != "logo.png" {
		t.Errorf("filename = %v", blob["filename"])
	}
	if blob["byte_size"].(float64) != float64(len(content)) {
		t.Errorf("byte_size = %v, want %d", blob["byte_size"], len(content))
	}
	sum := md5.Sum(content) //nolint:gosec // mirrors the Active Storage contract under test
	if blob["checksum"] != base64.StdEncoding.EncodeToString(sum[:]) {
		t.Errorf("checksum = %v, want MD5 of the file content", blob["checksum"])
	}

	if !bytes.Equal(*putBody, content) {
		t.Errorf("PUT body differs from the file content")
	}
	if !strings.Contains(out.String(), `"signed_id": "sgid-123"`) &&
		!strings.Contains(out.String(), `"signed_id":"sgid-123"`) {
		t.Errorf("output missing signed_id: %s", out.String())
	}
}

func TestUploadFromStdinRequiresFilename(t *testing.T) {
	authEnv(t)

	var out, errOut bytes.Buffer
	code, _ := runWith(strings.NewReader("data"), []string{"upload", "--stdin"}, &out, &errOut)

	if code == 0 {
		t.Fatalf("expected failure without --filename")
	}
	if !strings.Contains(errOut.String(), "--filename") {
		t.Errorf("stderr = %s", errOut.String())
	}
}

func TestUploadFromStdinUsesGivenFilename(t *testing.T) {
	authEnv(t)
	server, requests, _ := uploadServer(t)
	t.Setenv("LK_API_URL", server.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	code, _ := runWith(strings.NewReader("\x89PNG\r\n\x1a\ncontent"),
		[]string{"upload", "--stdin", "--filename", "icon.png"}, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut.String())
	}
	blob := (*requests)[0].body["blob"].(map[string]any)
	if blob["filename"] != "icon.png" {
		t.Errorf("filename = %v", blob["filename"])
	}
}

func TestUploadRefusesFileAndStdinTogether(t *testing.T) {
	authEnv(t)

	var out, errOut bytes.Buffer
	code := run([]string{"upload", "--file", "x.png", "--stdin"}, &out, &errOut)

	if code == 0 {
		t.Fatalf("expected failure with both --file and --stdin")
	}
}

func TestUploadRefusesEmptyContent(t *testing.T) {
	authEnv(t)
	path := writeUploadFile(t, "empty.png", nil)

	var out, errOut bytes.Buffer
	code := run([]string{"upload", "--file", path}, &out, &errOut)

	if code == 0 {
		t.Fatalf("expected failure on empty file")
	}
	if !strings.Contains(errOut.String(), "vazio") {
		t.Errorf("stderr = %s", errOut.String())
	}
}

func TestUploadSurfacesBackendError(t *testing.T) {
	authEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"errors":["nope"]}`))
	}))
	t.Cleanup(server.Close)
	t.Setenv("LK_API_URL", server.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	path := writeUploadFile(t, "logo.png", []byte("\x89PNGdata"))

	var out, errOut bytes.Buffer
	code := run([]string{"upload", "--file", path}, &out, &errOut)

	if code == 0 {
		t.Fatalf("expected failure on 422")
	}
	if !strings.Contains(errOut.String(), "422") {
		t.Errorf("stderr = %s", errOut.String())
	}
}

func TestUploadContentTypeFromExtension(t *testing.T) {
	pdf := []byte("%PDF-1.4 fake")
	zipBytes := []byte("PK\x03\x04fake office zip")
	png := []byte("\x89PNG\r\n\x1a\nfakepng.")
	cases := []struct {
		name    string
		content []byte
		want    string
	}{
		{"contrato.pdf", pdf, "application/pdf"},
		{"contrato.docx", zipBytes, "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"planilha.xlsx", zipBytes, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{"apresentacao.pptx", zipBytes, "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
		{"antigo.doc", []byte("\xd0\xcf\x11\xe0fake ole"), "application/msword"},
		{"antigo.xls", []byte("\xd0\xcf\x11\xe0fake ole"), "application/vnd.ms-excel"},
		{"LOGO.PNG", png, "image/png"},
		{"fornecedores.csv", []byte("cnpj,nome\n1,a\n"), "text/csv"},
		{"notas.txt", []byte("texto"), "text/plain"},
		{"pagina.html", []byte("<html></html>"), "text/html"},
		{"semextensao", pdf, "application/pdf"},
		{"semextensao-texto", []byte("texto simples"), "text/plain"},
		{"arquivo.extensaoinexistente", png, "image/png"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := uploadContentType(tc.name, tc.content); got != tc.want {
				t.Errorf("uploadContentType(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestUploadSendsContentTypeFromExtension(t *testing.T) {
	authEnv(t)
	server, requests, _ := uploadServer(t)
	t.Setenv("LK_API_URL", server.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")
	path := writeUploadFile(t, "contrato.docx", []byte("PK\x03\x04fake office zip"))

	var out, errOut bytes.Buffer
	code := run([]string{"upload", "--file", path}, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut.String())
	}
	blob := (*requests)[0].body["blob"].(map[string]any)
	want := "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	if blob["content_type"] != want {
		t.Errorf("content_type = %v, want %s", blob["content_type"], want)
	}
}

func TestUploadExplicitContentTypeWinsOverExtension(t *testing.T) {
	authEnv(t)
	server, requests, _ := uploadServer(t)
	t.Setenv("LK_API_URL", server.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")
	path := writeUploadFile(t, "contrato.docx", []byte("PK\x03\x04fake office zip"))

	var out, errOut bytes.Buffer
	code := run([]string{"upload", "--file", path, "--content-type", "application/zip"}, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut.String())
	}
	blob := (*requests)[0].body["blob"].(map[string]any)
	if blob["content_type"] != "application/zip" {
		t.Errorf("content_type = %v, want application/zip", blob["content_type"])
	}
}

func TestUploadTimesOutOnStalledStoragePut(t *testing.T) {
	authEnv(t)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(nil))
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			<-release
			return
		}
		_, _ = w.Write([]byte(`{"signed_id":"sgid-123","filename":"contrato.pdf","byte_size":13,` +
			`"content_type":"application/pdf","direct_upload":{"url":"` + server.URL + `/storage-put",` +
			`"headers":{"Content-Type":"application/pdf"}}}`))
	})
	t.Cleanup(func() {
		close(release)
		server.Close()
	})
	t.Setenv("LK_API_URL", server.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")
	path := writeUploadFile(t, "contrato.pdf", []byte("%PDF-1.4 fake"))

	var out, errOut bytes.Buffer
	code := run([]string{"upload", "--file", path, "--timeout", "50ms"}, &out, &errOut)

	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr: %s", code, errOut.String())
	}
	for _, want := range []string{"contrato.pdf", "50ms"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr missing %q: %s", want, errOut.String())
		}
	}
}

func TestUploadRejectsNonPositiveTimeout(t *testing.T) {
	authEnv(t)
	path := writeUploadFile(t, "contrato.pdf", []byte("%PDF-1.4 fake"))

	var out, errOut bytes.Buffer
	code := run([]string{"upload", "--file", path, "--timeout", "0s"}, &out, &errOut)

	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "--timeout") {
		t.Errorf("stderr = %s", errOut.String())
	}
}
