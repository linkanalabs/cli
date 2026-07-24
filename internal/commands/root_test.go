package commands

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSetVersion(t *testing.T) {
	orig := version
	defer func() { version = orig }()

	SetVersion("1.2.3")
	if version != "1.2.3" {
		t.Errorf("version = %q", version)
	}
	SetVersion("")
	if version != "1.2.3" {
		t.Errorf("empty SetVersion should not change version, got %q", version)
	}
}

func TestRunHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"--help"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "lk") {
		t.Errorf("help output = %q", out.String())
	}
}

func TestRunUnknownFormatIsRejectedBeforeTheRequest(t *testing.T) {
	authEnv(t)
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_abc_def")

	var out, errOut bytes.Buffer
	code := run([]string{"supplier", "list", "--format", "markdwon"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit code = %d, stdout = %q", code, out.String())
	}
	if got := hits.Load(); got != 0 {
		t.Errorf("backend was called %d time(s) despite the invalid format", got)
	}
	if !strings.Contains(errOut.String(), "auto|json|styled|markdown|ids|count") {
		t.Errorf("stderr should list the valid formats, got %q", errOut.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"nope"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(errOut.String(), "error:") {
		t.Errorf("stderr = %q", errOut.String())
	}
}
