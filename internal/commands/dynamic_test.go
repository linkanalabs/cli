package commands

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/manifest"
)

// fixtureManifest loads the rich test manifest from testdata.
func fixtureManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	data, err := os.ReadFile("testdata/manifest.json")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	m, err := manifest.Parse(data)
	if err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}
	return m
}

// swapManifest replaces the manifest-loading seam for the test's duration.
func swapManifest(t *testing.T, m *manifest.Manifest, err error) {
	t.Helper()
	prev := loadManifest
	loadManifest = func() (*manifest.Manifest, error) { return m, err }
	t.Cleanup(func() { loadManifest = prev })
}

// swapFixtureManifest points the seam at the testdata manifest.
func swapFixtureManifest(t *testing.T) {
	t.Helper()
	swapManifest(t, fixtureManifest(t), nil)
}

// findCommand walks the command tree by names, returning nil when absent.
func findCommand(root *cobra.Command, names ...string) *cobra.Command {
	cur := root
	for _, name := range names {
		var next *cobra.Command
		for _, c := range cur.Commands() {
			if c.Name() == name {
				next = c
				break
			}
		}
		if next == nil {
			return nil
		}
		cur = next
	}
	return cur
}

func TestDynamicTreeFromFixture(t *testing.T) {
	swapFixtureManifest(t)
	root := newRootCmd()

	group := findCommand(root, "widget")
	if group == nil {
		t.Fatal("dynamic group `widget` not registered")
	}
	if group.Short == "" {
		t.Error("dynamic group must have a derived Short")
	}
	if got := findCommand(root, "widget", "list"); got == nil || got.Short != "List widgets" {
		t.Errorf("widget list = %+v", got)
	}
	if got := findCommand(root, "widget", "note", "show"); got == nil || got.Short != "Show a widget note" {
		t.Errorf("widget note show (3 levels) = %+v", got)
	}
	if got := findCommand(root, "widget", "note", "show"); got != nil && got.Use != "show <widget_id> <note_id>" {
		t.Errorf("Use = %q, want positional path params in order", got.Use)
	}
}

func TestDynamicFlagTypes(t *testing.T) {
	swapFixtureManifest(t)
	root := newRootCmd()

	cases := []struct {
		cmd      []string
		flag     string
		flagType string
		required bool
	}{
		{[]string{"widget", "list"}, "q", "string", false},
		{[]string{"widget", "list"}, "page", "int64", false},
		{[]string{"widget", "list"}, "active", "bool", false},
		{[]string{"widget", "list"}, "state", "string", false},
		{[]string{"widget", "list"}, "tags", "stringArray", false},
		{[]string{"widget", "list"}, "filter", "string", false},
		{[]string{"widget", "create"}, "name", "string", true},
		{[]string{"widget", "create"}, "count", "int64", false},
		{[]string{"widget", "create"}, "enabled", "bool", false},
		{[]string{"widget", "create"}, "price", "string", false},
		{[]string{"widget", "create"}, "due_on", "string", false},
		{[]string{"widget", "create"}, "labels", "stringArray", false},
		{[]string{"widget", "create"}, "counts", "stringArray", false},
		{[]string{"widget", "create"}, "toggles", "stringArray", false},
		{[]string{"widget", "create"}, "metadata", "string", false},
		{[]string{"widget", "create"}, "items", "string", false},
		{[]string{"widget", "delete"}, "force", "bool", false},
	}
	for _, tc := range cases {
		cmd := findCommand(root, tc.cmd...)
		if cmd == nil {
			t.Fatalf("command %v not found", tc.cmd)
		}
		f := cmd.Flags().Lookup(tc.flag)
		if f == nil {
			t.Errorf("%v: flag --%s missing", tc.cmd, tc.flag)
			continue
		}
		if f.Value.Type() != tc.flagType {
			t.Errorf("%v --%s type = %q, want %q", tc.cmd, tc.flag, f.Value.Type(), tc.flagType)
		}
		required := len(f.Annotations[cobra.BashCompOneRequiredFlag]) > 0
		if required != tc.required {
			t.Errorf("%v --%s required = %t, want %t", tc.cmd, tc.flag, required, tc.required)
		}
	}
}

func TestDynamicEnumListedInUsage(t *testing.T) {
	swapFixtureManifest(t)
	root := newRootCmd()
	f := findCommand(root, "widget", "list").Flags().Lookup("state")
	if f == nil || !strings.Contains(f.Usage, "one of: draft|active|archived") {
		t.Errorf("state usage = %+v, want enum listed", f)
	}
}

func TestDynamicCollisionManualWins(t *testing.T) {
	swapFixtureManifest(t)
	root := newRootCmd()

	// Leaf collision: manual `supplier list` keeps its manual Short.
	if got := findCommand(root, "supplier", "list"); got == nil || !strings.Contains(got.Short, "GET /srm/suppliers") {
		t.Errorf("supplier list should stay manual, got %+v", got)
	}
	// Single-level collision: manual `whoami` keeps its manual Short and stays a leaf.
	whoami := findCommand(root, "whoami")
	if whoami == nil || whoami.Short != "Show the authenticated identity" {
		t.Errorf("whoami should stay manual, got %+v", whoami)
	}
	if whoami.HasSubCommands() {
		t.Error("whoami must not gain dynamic subcommands")
	}
	// Intermediate collision: the manual `version` leaf must not gain children.
	if v := findCommand(root, "version"); v == nil || v.HasSubCommands() {
		t.Errorf("version should stay a manual leaf, got %+v", v)
	}
}

func TestDynamicSkipsDuplicateDynamicCommand(t *testing.T) {
	m := fixtureManifest(t)
	dup := m.Endpoints[0] // widget list
	dup.Summary = "Duplicate that must be skipped"
	m.Endpoints = append(m.Endpoints, dup)
	swapManifest(t, m, nil)

	root := newRootCmd()
	if got := findCommand(root, "widget", "list"); got == nil || got.Short != "List widgets" {
		t.Errorf("first dynamic registration must win, got %+v", got)
	}
}

func TestDynamicManifestLoadErrorDisablesDynamic(t *testing.T) {
	swapManifest(t, nil, os.ErrNotExist)
	root := newRootCmd()
	if findCommand(root, "widget") != nil || findCommand(root, "identity") != nil {
		t.Error("no dynamic commands should register when the manifest fails to load")
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"--help"}, &out, &errOut); code != 0 {
		t.Fatalf("help must still work, exit = %d", code)
	}
}

func TestDynamicEmbeddedManifestRegisters(t *testing.T) {
	// No seam swap: the real embedded manifest drives the tree.
	root := newRootCmd()
	if got := findCommand(root, "identity", "show"); got == nil {
		t.Fatal("embedded manifest should register `identity show`")
	}
	if got := findCommand(root, "settings", "email-message", "update"); got == nil {
		t.Fatal("embedded manifest should register `settings email-message update`")
	}
}

func TestDynamicHelpIsLLMFirst(t *testing.T) {
	swapFixtureManifest(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"widget", "create", "--help"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	help := out.String()
	for _, want := range []string{
		"Creates a widget for the buyer.",
		"Endpoint:",
		"POST /widgets",
		"Auth: pat",
		"Parameters:",
		"name", "string", "yes", "Widget name",
		"array of object",
		"Response:",
		"201 with the widget; 422 {errors} on validation.",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("help missing %q:\n%s", want, help)
		}
	}
}

func TestDynamicHelpListsPositionalArgs(t *testing.T) {
	swapFixtureManifest(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"widget", "note", "show", "--help"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	help := out.String()
	for _, want := range []string{"show <widget_id> <note_id>", "Arguments", "widget_id", "note_id"} {
		if !strings.Contains(help, want) {
			t.Errorf("help missing %q:\n%s", want, help)
		}
	}
}
