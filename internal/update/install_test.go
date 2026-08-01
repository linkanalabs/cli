package update

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeCaskroom builds the layout Homebrew really produces for a cask:
//
//	<root>/Caskroom/lk/0.7.0/lk            the binary
//	<root>/Caskroom/lk/.metadata/INSTALL_RECEIPT.json
//
// It returns the binary path.
func fakeCaskroom(t *testing.T, receipt string) string {
	t.Helper()
	root := t.TempDir()
	binDir := filepath.Join(root, "Caskroom", "lk", "0.7.0")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(binDir, "lk")
	if err := os.WriteFile(bin, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if receipt != "" {
		metaDir := filepath.Join(root, "Caskroom", "lk", ".metadata")
		if err := os.MkdirAll(metaDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(metaDir, "INSTALL_RECEIPT.json"), []byte(receipt), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return bin
}

const goodReceipt = `{
  "installed_on_request": true,
  "source": {
    "tap": "linkanalabs/tap",
    "version": "0.7.0",
    "path": "/opt/homebrew/Library/Taps/linkanalabs/homebrew-tap/Casks/lk.rb"
  }
}`

// stubExecutable points Detect at a given path for the duration of a test.
func stubExecutable(t *testing.T, path string, err error) {
	t.Helper()
	prev := osExecutable
	osExecutable = func() (string, error) { return path, err }
	t.Cleanup(func() { osExecutable = prev })
}

func TestDetectHomebrewCask(t *testing.T) {
	bin := fakeCaskroom(t, goodReceipt)
	stubExecutable(t, bin, nil)

	got, err := Detect()
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if got.Method != MethodHomebrewCask {
		t.Errorf("Method = %q, want %q", got.Method, MethodHomebrewCask)
	}
	if want := "/opt/homebrew/Library/Taps/linkanalabs/homebrew-tap/Casks/lk.rb"; got.CaskPath() != want {
		t.Errorf("CaskPath() = %q, want %q", got.CaskPath(), want)
	}
}

// Detect must not read the receipt: it runs on every lk invocation, while the
// cask path is only needed once a day.
func TestDetectDoesNotReadTheReceipt(t *testing.T) {
	stubExecutable(t, fakeCaskroom(t, goodReceipt), nil)

	var reads int
	prev := readFile
	readFile = func(name string) ([]byte, error) {
		reads++
		return prev(name)
	}
	t.Cleanup(func() { readFile = prev })

	got, err := Detect()
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if reads != 0 {
		t.Errorf("Detect() read %d files, want 0", reads)
	}
	if got.CaskPath() == "" {
		t.Error("CaskPath() found nothing once it was actually asked for")
	}
	if reads == 0 {
		t.Error("CaskPath() did not read the receipt")
	}
}

// A non-cask install has no receipt to consult.
func TestCaskPathEmptyOutsideACask(t *testing.T) {
	in := &Install{Method: MethodOther, resolved: "/usr/local/bin/lk"}
	if got := in.CaskPath(); got != "" {
		t.Errorf("CaskPath() = %q, want empty", got)
	}
}

// The binary is reached through the symlink Homebrew drops in <prefix>/bin.
// macOS does not resolve symlinks in os.Executable, so detection has to.
func TestDetectResolvesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need elevation on windows")
	}
	bin := fakeCaskroom(t, goodReceipt)

	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "lk")
	if err := os.Symlink(bin, link); err != nil {
		t.Fatal(err)
	}
	stubExecutable(t, link, nil)

	got, err := Detect()
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if got.Method != MethodHomebrewCask {
		t.Errorf("Method = %q, want %q (symlink not resolved)", got.Method, MethodHomebrewCask)
	}
}

// A cask install stays a cask install even when the receipt is missing or
// corrupt: the path already proved it. Only the tap metadata is lost.
func TestDetectCaskWithoutUsableReceipt(t *testing.T) {
	cases := []struct {
		name    string
		receipt string
	}{
		{"missing", ""},
		{"malformed", "{not json"},
		{"empty source", `{"source":{}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stubExecutable(t, fakeCaskroom(t, c.receipt), nil)

			got, err := Detect()
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			if got.Method != MethodHomebrewCask {
				t.Errorf("Method = %q, want %q", got.Method, MethodHomebrewCask)
			}
			if got.CaskPath() != "" {
				t.Errorf("expected no cask path, got %q", got.CaskPath())
			}
		})
	}
}

func TestDetectHomebrewFormula(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Cellar", "lk", "0.7.0", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "lk")
	if err := os.WriteFile(bin, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubExecutable(t, bin, nil)

	got, err := Detect()
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if got.Method != MethodHomebrewFormula {
		t.Errorf("Method = %q, want %q", got.Method, MethodHomebrewFormula)
	}
}

func TestDetectOther(t *testing.T) {
	// What scripts/install.sh produces: a plain file in ~/.local/bin.
	dir := t.TempDir()
	bin := filepath.Join(dir, "lk")
	if err := os.WriteFile(bin, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubExecutable(t, bin, nil)

	got, err := Detect()
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if got.Method != MethodOther {
		t.Errorf("Method = %q, want %q", got.Method, MethodOther)
	}
}

// "Caskroom" must match a whole path segment. A user directory that merely
// contains the word is not a Homebrew install.
func TestDetectSubstringIsNotASegment(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "MyCaskroomBackup")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "lk")
	if err := os.WriteFile(bin, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubExecutable(t, bin, nil)

	got, err := Detect()
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if got.Method != MethodOther {
		t.Errorf("Method = %q, want %q", got.Method, MethodOther)
	}
}

func TestDetectExecutableError(t *testing.T) {
	stubExecutable(t, "", errors.New("boom"))

	if _, err := Detect(); err == nil {
		t.Fatal("expected an error when the executable path is unknown")
	}
}

// A dangling symlink must not sink detection: fall back to the unresolved path.
func TestDetectFallsBackWhenSymlinkUnresolvable(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "Caskroom", "lk", "0.7.0", "lk")
	stubExecutable(t, missing, nil)

	got, err := Detect()
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if got.Method != MethodHomebrewCask {
		t.Errorf("Method = %q, want %q", got.Method, MethodHomebrewCask)
	}
}
