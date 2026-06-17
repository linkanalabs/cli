package commands

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/linkanalabs/cli/internal/auth"
)

const supplierJSON = `{"id":"s_1","name":"Acme","identifier":"12345","legal_entity":"Acme Ltda","state":"active","tags":[{"id":"t_1","display_name":"Critical"}]}`

// supplierServer serves the SRM supplier endpoints with the given status/body.
func supplierServer(t *testing.T, listStatus int, listBody string, showStatus int, showBody string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/srm/suppliers.json":
			w.WriteHeader(listStatus)
			_, _ = w.Write([]byte(listBody))
		case "/srm/suppliers/s_1/panel.json":
			w.WriteHeader(showStatus)
			_, _ = w.Write([]byte(showBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSupplierListSuccess(t *testing.T) {
	authEnv(t)
	srv := supplierServer(t, http.StatusOK, "["+supplierJSON+"]", http.StatusOK, supplierJSON)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	code := run([]string{"supplier", "list", "--format", "json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"Acme"`) || !strings.Contains(out.String(), `"Critical"`) {
		t.Errorf("output = %q", out.String())
	}
}

func TestSupplierListStyled(t *testing.T) {
	authEnv(t)
	srv := supplierServer(t, http.StatusOK, "["+supplierJSON+"]", http.StatusOK, supplierJSON)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	code := run([]string{"supplier", "list", "--format", "styled"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	for _, want := range []string{"Acme", "12345", "active"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("styled output missing %q: %q", want, out.String())
		}
	}
}

func TestSupplierListEmptyStyled(t *testing.T) {
	authEnv(t)
	srv := supplierServer(t, http.StatusOK, "[]", http.StatusOK, supplierJSON)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	code := run([]string{"supplier", "list", "--format", "styled"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "No suppliers") {
		t.Errorf("expected empty hint, got %q", out.String())
	}
}

func TestSupplierListNoToken(t *testing.T) {
	authEnv(t)
	srv := supplierServer(t, http.StatusOK, "[]", http.StatusOK, supplierJSON)
	t.Setenv("LK_API_URL", srv.URL)

	var out, errOut bytes.Buffer
	code := run([]string{"supplier", "list"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "lk auth login") {
		t.Errorf("stderr should hint login: %q", errOut.String())
	}
}

func TestSupplierListUnauthorized(t *testing.T) {
	authEnv(t)
	srv := supplierServer(t, http.StatusUnauthorized, "", http.StatusOK, supplierJSON)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_bad_token")

	var out, errOut bytes.Buffer
	code := run([]string{"supplier", "list"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "lk auth login") {
		t.Errorf("stderr should hint re-login: %q", errOut.String())
	}
}

func TestSupplierListLoadError(t *testing.T) {
	authEnv(t)
	swapAuthLoad(t, func(string) (string, auth.Source, error) {
		return "", auth.SourceNone, errors.New("keychain locked")
	})
	var out, errOut bytes.Buffer
	if code := run([]string{"supplier", "list"}, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestSupplierListServerError(t *testing.T) {
	authEnv(t)
	srv := supplierServer(t, http.StatusInternalServerError, "", http.StatusOK, supplierJSON)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	if code := run([]string{"supplier", "list"}, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestSupplierShowSuccess(t *testing.T) {
	authEnv(t)
	srv := supplierServer(t, http.StatusOK, "[]", http.StatusOK, supplierJSON)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	code := run([]string{"supplier", "show", "s_1", "--format", "json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"Acme"`) {
		t.Errorf("output = %q", out.String())
	}
}

func TestSupplierShowStyled(t *testing.T) {
	authEnv(t)
	srv := supplierServer(t, http.StatusOK, "[]", http.StatusOK, supplierJSON)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	code := run([]string{"supplier", "show", "s_1", "--format", "styled"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	for _, want := range []string{"Acme", "s_1", "12345", "Acme Ltda", "active", "Critical"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("styled output missing %q: %q", want, out.String())
		}
	}
}

func TestSupplierShowNoTags(t *testing.T) {
	authEnv(t)
	body := `{"id":"s_2","name":"Beta","identifier":"99","legal_entity":"Beta SA","state":"pending","tags":[]}`
	srv := supplierServer(t, http.StatusOK, "[]", http.StatusOK, body)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	code := run([]string{"supplier", "show", "s_1", "--format", "styled"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "Beta") {
		t.Errorf("output = %q", out.String())
	}
}

func TestSupplierShowNoToken(t *testing.T) {
	authEnv(t)
	srv := supplierServer(t, http.StatusOK, "[]", http.StatusOK, supplierJSON)
	t.Setenv("LK_API_URL", srv.URL)

	var out, errOut bytes.Buffer
	code := run([]string{"supplier", "show", "s_1"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "lk auth login") {
		t.Errorf("stderr should hint login: %q", errOut.String())
	}
}

func TestSupplierShowUnauthorized(t *testing.T) {
	authEnv(t)
	srv := supplierServer(t, http.StatusOK, "[]", http.StatusUnauthorized, "")
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_bad_token")

	var out, errOut bytes.Buffer
	code := run([]string{"supplier", "show", "s_1"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "lk auth login") {
		t.Errorf("stderr should hint re-login: %q", errOut.String())
	}
}

func TestSupplierShowLoadError(t *testing.T) {
	authEnv(t)
	swapAuthLoad(t, func(string) (string, auth.Source, error) {
		return "", auth.SourceNone, errors.New("keychain locked")
	})
	var out, errOut bytes.Buffer
	if code := run([]string{"supplier", "show", "s_1"}, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestSupplierShowServerError(t *testing.T) {
	authEnv(t)
	srv := supplierServer(t, http.StatusOK, "[]", http.StatusInternalServerError, "")
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	if code := run([]string{"supplier", "show", "s_1"}, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestSupplierListViewMarshalNil(t *testing.T) {
	// A nil slice must still render as a bare empty array, never null.
	b, err := supplierListView{}.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error: %v", err)
	}
	if string(b) != "[]" {
		t.Errorf("MarshalJSON() = %q, want []", b)
	}
}

func TestSupplierShowRequiresArg(t *testing.T) {
	authEnv(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"supplier", "show"}, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}
