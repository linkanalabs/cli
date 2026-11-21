package commands

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/linkanalabs/cli/internal/manifest"
)

func searchJSON(t *testing.T, args ...string) []searchResult {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := run(append([]string{"cli", "search", "--format", "json"}, args...), &out, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	var results []searchResult
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		t.Fatalf("parsing %q: %v", out.String(), err)
	}
	return results
}

func commandsOf(results []searchResult) []string {
	names := make([]string, len(results))
	for i, r := range results {
		names[i] = r.Command
	}
	return names
}

func embeddedManifest(t *testing.T) {
	t.Helper()
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	swapManifest(t, m, nil)
}

func TestCliSearchFindsTheCommandsOfThePortugueseTask(t *testing.T) {
	embeddedManifest(t)
	cases := map[string]string{
		"aprovar documento bancário":                          "document bank-information-override",
		"anexar arquivo em campo personalizado do fornecedor": "supplier buyer-document create",
		"reiniciar homologação":                               "qualification versionate",
		"listar fornecedores":                                 "supplier list",
		"convidar usuário":                                    "settings company-user invite create",
		"subir arquivo":                                       "upload",
		"quem sou eu":                                         "whoami",
	}
	for query, want := range cases {
		got := commandsOf(searchJSON(t, query))
		found := false
		for _, c := range got {
			found = found || c == want
		}
		if !found {
			t.Errorf("%q: %q not in %v", query, want, got)
		}
	}
}

func TestCliSearchRanksTheNameAboveTheDescription(t *testing.T) {
	embeddedManifest(t)

	results := searchJSON(t, "listar fornecedores")

	if results[0].Command != "supplier list" {
		t.Errorf("first = %q", results[0].Command)
	}
	if results[0].Usage != "lk supplier list [flags]" || results[0].Summary == "" {
		t.Errorf("result = %+v", results[0])
	}
}

func TestCliSearchFindsABackendCommandUnknownToThisBinary(t *testing.T) {
	m, err := manifest.Parse([]byte(backendOnlyManifest))
	if err != nil {
		t.Fatal(err)
	}
	swapManifest(t, m, nil)

	results := searchJSON(t, "listar gadgets")

	if len(results) == 0 || results[0].Command != "gadget list" {
		t.Errorf("results = %v", commandsOf(results))
	}
}

func TestCliSearchLimitNoMatchAndNeverListsItself(t *testing.T) {
	embeddedManifest(t)

	if got := searchJSON(t, "--limit", "2", "documento"); len(got) != 2 {
		t.Errorf("limit 2 returned %d", len(got))
	}
	if got := searchJSON(t, "xyzzy"); len(got) != 0 {
		t.Errorf("no match returned %v", commandsOf(got))
	}
	if got := searchJSON(t, "de", "para"); len(got) != 0 {
		t.Errorf("only stop words returned %v", commandsOf(got))
	}
	for _, c := range commandsOf(searchJSON(t, "--limit", "200", "search", "buscar", "comando")) {
		if c == "cli search" || c == "help" || c == "completion bash" {
			t.Errorf("listed %q", c)
		}
	}
}

func TestCliSearchStyledAndErrors(t *testing.T) {
	embeddedManifest(t)
	var out, errOut bytes.Buffer

	run([]string{"cli", "search", "--format", "styled", "listar fornecedores"}, &out, &errOut)
	if !strings.HasPrefix(out.String(), "lk supplier list [flags]") {
		t.Errorf("styled = %q", out.String())
	}

	out.Reset()
	run([]string{"cli", "search", "--format", "styled", "xyzzy"}, &out, &errOut)
	if !strings.Contains(out.String(), "no command matches") {
		t.Errorf("styled empty = %q", out.String())
	}

	errOut.Reset()
	if code := run([]string{"cli", "search", "--limit", "0", "x"}, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "--limit") {
		t.Errorf("limit 0: exit %d stderr %q", code, errOut.String())
	}
	if code := run([]string{"cli", "search"}, &out, &errOut); code != 1 {
		t.Errorf("no query: exit %d", code)
	}
}

func TestRootHelpPointsToTheSearch(t *testing.T) {
	var out, errOut bytes.Buffer
	run([]string{"--help"}, &out, &errOut)

	if !strings.Contains(out.String(), `lk cli search "<what you want to do>"`) {
		t.Errorf("help = %q", out.String())
	}
}

func TestCliSearchUnderstandsPortuguesePlurals(t *testing.T) {
	embeddedManifest(t)
	cases := map[string]string{
		"listar fornecedores":         "supplier list",
		"revisar aprovações":          "document review",
		"mensagens de e-mail":         "settings email-message list",
		"categorias dos fornecedores": "settings category list",
	}
	for query, want := range cases {
		found := false
		for _, c := range commandsOf(searchJSON(t, query)) {
			found = found || c == want
		}
		if !found {
			t.Errorf("%q: %q not found", query, want)
		}
	}
	for in, want := range map[string]string{"fornecedore": "fornecedor", "aprovacoe": "aprovacao", "mensagen": "mensagem", "papei": "papel", "documento": "documento"} {
		if got := portugueseSingular(in); got != want {
			t.Errorf("portugueseSingular(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCliSearchStyledEscapesControlCharacters(t *testing.T) {
	got := searchResults{{Usage: "lk x", Summary: "evil\x1b[2Jsummary\nfake line"}}.Styled()
	if strings.ContainsAny(got[:len(got)-1], "\x1b\n") {
		t.Errorf("styled output carries control characters: %q", got)
	}
}

func TestTokensStripAccentsAndPlurals(t *testing.T) {
	got := strings.Join(tokens("Documentos Bancários, process ação"), " ")
	if got != "documento bancario process acao" {
		t.Errorf("tokens = %q", got)
	}
}
