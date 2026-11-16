package commands

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/output"
)

const defaultSearchLimit = 5

// Weights of a query term found in each part of a command: the name says what
// the command is, the summary what it does, the long help everything else.
const (
	weightNameExact  = 10
	weightNamePrefix = 6
	weightSummary    = 3
	weightLong       = 1
)

// searchResult is one command returned by `lk cli search`.
type searchResult struct {
	Command string `json:"command"`
	Usage   string `json:"usage"`
	Summary string `json:"summary"`
	score   int
}

type searchResults []searchResult

// Styled renders one command per line, the usage aligned before its summary.
func (rs searchResults) Styled() string {
	if len(rs) == 0 {
		return "no command matches; try other words or `lk --help`\n"
	}
	width := 0
	for _, r := range rs {
		width = max(width, len(r.Usage))
	}
	var b strings.Builder
	for _, r := range rs {
		fmt.Fprintf(&b, "%-*s  %s\n", width, r.Usage, r.Summary)
	}
	return b.String()
}

func newCliCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cli",
		Short: "Find your way around lk",
	}
	cmd.AddCommand(newCliSearchCmd())
	return cmd
}

func newCliSearchCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "search <what you want to do>",
		Short: "List the commands that best match a task, described in Portuguese or English",
		Long: "Searches every lk command — the backend's ones included, so a command exposed\n" +
			"after this binary was built shows up too — and lists the best matches.\n" +
			"The command name counts more than its summary, and the summary more than\n" +
			"the rest of its help. Portuguese words of the domain are matched to the\n" +
			"English command names (fornecedor → supplier, aprovar → review, …).\n\n" +
			"Next step: `lk <command> --help` for the flags of the one you pick.",
		Example: `  lk cli search "aprovar documento bancário"
  lk cli search "anexar arquivo em campo personalizado do fornecedor"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit < 1 {
				return fmt.Errorf("--limit must be at least 1")
			}
			results := searchCommands(cmd.Root(), strings.Join(args, " "), limit)
			return output.Render(cmd.OutOrStdout(), formatFlag(cmd), results)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", defaultSearchLimit, "how many commands to list")
	return cmd
}

// searchCommands scores every runnable command of the tree against the query
// and returns the best ones, highest score first.
func searchCommands(root *cobra.Command, query string, limit int) searchResults {
	terms := queryTerms(query)
	results := searchResults{}
	if len(terms) == 0 {
		return results
	}
	walkRunnable(root, func(c *cobra.Command) {
		if score := scoreCommand(c, root, terms); score > 0 {
			results = append(results, searchResult{
				Command: commandName(c, root),
				Usage:   strings.TrimSpace("lk " + strings.TrimPrefix(c.UseLine(), root.Name()+" ")),
				Summary: c.Short,
				score:   score,
			})
		}
	})
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		if len(results[i].Command) != len(results[j].Command) {
			return len(results[i].Command) < len(results[j].Command)
		}
		return results[i].Command < results[j].Command
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

// walkRunnable visits every command that does something, skipping cobra's own
// help and completion and the search itself.
func walkRunnable(c *cobra.Command, visit func(*cobra.Command)) {
	for _, child := range c.Commands() {
		if child.Hidden || child.Name() == "help" || child.Name() == "completion" {
			continue
		}
		if child.Runnable() && !isSearchCommand(child) {
			visit(child)
		}
		walkRunnable(child, visit)
	}
}

func isSearchCommand(c *cobra.Command) bool {
	return c.Name() == "search" && c.Parent().Name() == "cli"
}

// commandName is the command path without the binary name: "supplier list".
func commandName(c, root *cobra.Command) string {
	return strings.TrimPrefix(c.CommandPath(), root.Name()+" ")
}

// scoreCommand adds, for each query term, the best weight any of its forms
// (the word itself and its synonyms) reaches in the command.
func scoreCommand(c, root *cobra.Command, terms [][]string) int {
	name := tokenSet(strings.ReplaceAll(commandName(c, root), "-", " "))
	summary := tokenSet(c.Short)
	long := tokenSet(c.Long)
	score := 0
	for _, forms := range terms {
		best := 0
		for _, f := range forms {
			best = max(best, termWeight(f, name, summary, long))
		}
		score += best
	}
	return score
}

func termWeight(term string, name, summary, long map[string]bool) int {
	switch {
	case name[term]:
		return weightNameExact
	case hasPrefixToken(name, term):
		return weightNamePrefix
	case summary[term]:
		return weightSummary
	case long[term]:
		return weightLong
	}
	return 0
}

// hasPrefixToken matches a term of at least 4 letters that starts a word of the
// command: "qualif" finds "qualification". Only that way round, so the synonym
// "versionate" does not find `lk version`.
func hasPrefixToken(tokens map[string]bool, term string) bool {
	if len(term) < 4 {
		return false
	}
	for t := range tokens {
		if strings.HasPrefix(t, term) {
			return true
		}
	}
	return false
}

// queryTerms turns the query into one list per meaningful word: the word in
// its singular form followed by its English synonyms.
func queryTerms(query string) [][]string {
	var terms [][]string
	for _, w := range tokens(query) {
		if stopWords[w] {
			continue
		}
		forms := []string{w}
		forms = append(forms, synonyms[w]...)
		terms = append(terms, forms)
	}
	return terms
}

func tokenSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, t := range tokens(s) {
		set[t] = true
	}
	return set
}

// tokens lowercases, strips accents, splits on anything that is not a letter or
// digit and drops a plural "s", so "Documentos bancários" gives
// ["documento", "bancario"].
func tokens(s string) []string {
	fields := strings.FieldsFunc(stripAccents(strings.ToLower(s)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := fields[:0]
	for _, f := range fields {
		if len(f) > 3 && strings.HasSuffix(f, "s") && !strings.HasSuffix(f, "ss") {
			f = strings.TrimSuffix(f, "s")
		}
		out = append(out, f)
	}
	return out
}

var accentReplacer = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "ê", "e", "è", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n",
)

func stripAccents(s string) string { return accentReplacer.Replace(s) }

var stopWords = map[string]bool{
	"a": true, "o": true, "e": true, "de": true, "da": true, "do": true, "das": true,
	"em": true, "no": true, "na": true, "um": true, "uma": true, "para": true,
	"pra": true, "por": true, "com": true, "que": true, "como": true, "eu": true,
	"quero": true, "sou": true, "meu": true, "minha": true, "the": true, "of": true, "to": true, "in": true, "on": true,
	"an": true, "for": true, "and": true, "or": true, "with": true,
}

// synonyms maps Portuguese words of the domain, without accents and in the
// singular, to the English words the command names and summaries use.
var synonyms = map[string][]string{
	"fornecedor":    {"supplier"},
	"documento":     {"document"},
	"campo":         {"field", "buyer"},
	"personalizado": {"buyer", "custom"},
	"arquivo":       {"file", "upload"},
	"anexo":         {"file", "upload"},
	"anexar":        {"file", "upload"},
	"subir":         {"upload"},
	"enviar":        {"upload", "send"},
	"aprovar":       {"review", "clear", "approve", "approval"},
	"aprovacao":     {"approval", "review"},
	"reprovar":      {"review", "not", "refuse"},
	"revisar":       {"review"},
	"revisao":       {"review"},
	"desfazer":      {"revert"},
	"reverter":      {"revert"},
	"bancario":      {"bank"},
	"bancaria":      {"bank"},
	"banco":         {"bank"},
	"conta":         {"bank", "account"},
	"homologacao":   {"qualification"},
	"rehomologacao": {"qualification", "versionate"},
	"reiniciar":     {"versionate", "restart"},
	"categoria":     {"category"},
	"risco":         {"risk"},
	"usuario":       {"user", "company"},
	"convite":       {"invite"},
	"convidar":      {"invite"},
	"etapa":         {"step", "approval"},
	"nivel":         {"tier"},
	"condicao":      {"condition"},
	"motivo":        {"reason"},
	"recusa":        {"reason", "refuse"},
	"grupo":         {"grouper", "group"},
	"marcador":      {"tag"},
	"etiqueta":      {"tag"},
	"notificacao":   {"notification"},
	"email":         {"email", "message"},
	"mensagem":      {"message"},
	"avaliacao":     {"performance", "evaluation"},
	"desempenho":    {"performance"},
	"pilar":         {"pillar"},
	"pergunta":      {"ask", "question"},
	"formulario":    {"form", "supplier", "field"},
	"consulta":      {"certificate"},
	"certidao":      {"certificate"},
	"lista":         {"list"},
	"listar":        {"list"},
	"ver":           {"show"},
	"mostrar":       {"show"},
	"detalhe":       {"show"},
	"criar":         {"create"},
	"cadastrar":     {"create"},
	"adicionar":     {"create", "add"},
	"preencher":     {"create", "update"},
	"atualizar":     {"update", "override"},
	"editar":        {"update"},
	"alterar":       {"update"},
	"mudar":         {"update"},
	"trocar":        {"update"},
	"apagar":        {"delete", "destroy", "disable"},
	"remover":       {"delete", "destroy", "remove", "disable"},
	"desativar":     {"disable"},
	"ativar":        {"enable"},
	"arquivar":      {"disable"},
	"ordenar":       {"reorder"},
	"reordenar":     {"reorder"},
	"duplicar":      {"duplicate"},
	"vencimento":    {"expires"},
	"validade":      {"expires"},
	"renovacao":     {"renew"},
	"solicitar":     {"request"},
	"cancelar":      {"cancel"},
	"impersonar":    {"impersonate"},
	"importar":      {"import", "csv"},
	"exportar":      {"export", "csv"},
	"empresa":       {"company"},
	"login":         {"auth", "login"},
	"quem":          {"whoami"},
}
