package commands

import (
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/auth"
	"github.com/linkanalabs/cli/internal/client"
	"github.com/linkanalabs/cli/internal/output"
)

// certificateListKind describes one of the two CSV-fed certificate lists: which
// endpoint receives the chunks, which body root wraps them and which CSV columns
// feed a row's values.
type certificateListKind struct {
	group       string
	path        string
	bodyRoot    string
	valueKeys   []string
	headerToKey map[string]string
}

var restrictionListKind = certificateListKind{
	group:     "restriction-list",
	path:      "/srm_settings/certificates_internal_restriction_lists_csv_imports",
	bodyRoot:  "certificates_internal_restriction_lists_csv_import",
	valueKeys: []string{"cnpj", "cpf", "nome"},
	headerToKey: map[string]string{
		"cnpj": "cnpj",
		"cpf":  "cpf",
		"nome": "nome",
		"name": "nome",
	},
}

var cnaeListKind = certificateListKind{
	group:       "cnae-list",
	path:        "/srm_settings/certificates_cnae_lists_csv_imports",
	bodyRoot:    "certificates_cnae_lists_csv_import",
	valueKeys:   []string{"cnae"},
	headerToKey: map[string]string{"cnae": "cnae"},
}

// csvImportView is the CsvImport payload the backend answers with, both on a
// chunk POST and on the progress endpoint.
type csvImportView struct {
	Identifier        string  `json:"identifier"`
	Filename          string  `json:"filename"`
	Status            string  `json:"status"`
	TotalCount        int     `json:"total_count"`
	ImportedCount     int     `json:"imported_count"`
	FailedCount       int     `json:"failed_count"`
	DuplicatedCount   int     `json:"duplicated_count"`
	CompletedAt       *string `json:"completed_at"`
	CertificateID     string  `json:"setting_certificate_id"`
	FailedRowsFileURL string  `json:"failed_rows_file_url,omitempty"`
}

// terminalImportStatuses are the states where an import stops moving. `deleted`
// belongs here: a `clear` on the certificate marks every import deleted, so a
// poll waiting only for `completed` would never return.
var terminalImportStatuses = map[string]bool{"completed": true, "failed": true, "deleted": true}

// importPollInterval is a variable so tests do not wait on it.
var importPollInterval = 10 * time.Second

// errImportUnfinished marks an import that did not end in `completed`. The
// command still prints the final state, then exits non-zero, so the caller sees
// what happened and anything chaining on the exit code stops.
var errImportUnfinished = errors.New("o import não terminou em completed")

// newCertificateListsCmds builds the `settings certificate <list> import` group
// chain. It mounts before registerDynamic, which then reuses these groups and
// hangs the raw `import-chunk` leaf next to the high-level `import`.
func newCertificateListsCmds() *cobra.Command {
	settings := &cobra.Command{Use: "settings", Short: "settings commands"}
	certificate := &cobra.Command{Use: "certificate", Short: "certificate commands"}
	settings.AddCommand(certificate)

	for _, kind := range []certificateListKind{restrictionListKind, cnaeListKind} {
		group := &cobra.Command{Use: kind.group, Short: kind.group + " commands"}
		group.AddCommand(newCertificateListImportCmd(kind))
		certificate.AddCommand(group)
	}
	return settings
}

// newCertificateListImportCmd builds the high-level import for one list kind: it
// reads the CSV, splits it into chunks, posts every chunk under a single
// identifier and follows the progress to a terminal status.
func newCertificateListImportCmd(kind certificateListKind) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: fmt.Sprintf("Importar um CSV para a %s de um certificado", kind.group),
		Long: fmt.Sprintf("Lê o CSV, fatia em chunks e envia todos sob um mesmo identifier, "+
			"depois acompanha o progresso até o fim.\n\n"+
			"O CSV vem de --file ou de --stdin (exatamente um dos dois) e precisa de cabeçalho; "+
			"as colunas usadas são: %s.\n"+
			"As linhas são processadas em fila, então o comando segue acompanhando e sai quando o "+
			"import termina — use --no-wait para sair logo após enviar.", strings.Join(kind.valueKeys, ", ")),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCertificateListImport(cmd, kind)
		},
	}
	cmd.Flags().String("id", "", "id do certificado, de: lk settings document list")
	cmd.Flags().String("file", "", "caminho do CSV a importar")
	cmd.Flags().Bool("stdin", false, "lê o CSV da entrada padrão")
	cmd.Flags().Int("chunk-size", 500, "linhas por request")
	cmd.Flags().Bool("no-wait", false, "envia os chunks e sai, sem acompanhar o progresso")
	cmd.Flags().Duration("timeout", 15*time.Minute, "tempo máximo acompanhando o progresso")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func runCertificateListImport(cmd *cobra.Command, kind certificateListKind) error {
	certificateID, _ := cmd.Flags().GetString("id")
	filePath, _ := cmd.Flags().GetString("file")
	fromStdin, _ := cmd.Flags().GetBool("stdin")
	chunkSize, _ := cmd.Flags().GetInt("chunk-size")
	noWait, _ := cmd.Flags().GetBool("no-wait")
	timeout, _ := cmd.Flags().GetDuration("timeout")

	if chunkSize <= 0 {
		return fmt.Errorf("--chunk-size tem que ser maior que zero")
	}

	reader, closeSource, err := openCSVSource(cmd, filePath, fromStdin)
	if err != nil {
		return err
	}
	defer closeSource()

	rows, err := readCertificateListRows(reader, kind)
	if err != nil {
		return err
	}

	api, imp, err := resolveAPI()
	if err != nil {
		return err
	}

	identifier, err := newImportIdentifier()
	if err != nil {
		return err
	}
	filename := "stdin.csv"
	if filePath != "" {
		filename = filePath
	}

	accepted, err := sendCertificateListChunks(cmd, api, imp, kind, certificateID, identifier, filename, rows, chunkSize)
	if err != nil {
		return err
	}
	if noWait {
		return output.Render(cmd.OutOrStdout(), formatFlag(cmd), accepted)
	}

	final, followErr := followCertificateListImport(cmd, api, imp, certificateID, identifier, timeout)
	if followErr != nil && !errors.Is(followErr, errImportUnfinished) {
		return followErr
	}
	if err := output.Render(cmd.OutOrStdout(), formatFlag(cmd), final); err != nil {
		return err
	}
	return followErr
}

// openCSVSource resolves --file / --stdin into a reader, refusing both or neither.
func openCSVSource(cmd *cobra.Command, filePath string, fromStdin bool) (io.Reader, func(), error) {
	switch {
	case filePath != "" && fromStdin:
		return nil, nil, fmt.Errorf("use --file ou --stdin, não os dois")
	case filePath == "" && !fromStdin:
		return nil, nil, fmt.Errorf("informe --file <caminho.csv> ou --stdin")
	case fromStdin:
		return cmd.InOrStdin(), func() {}, nil
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("abrindo %s: %w", filePath, err)
	}
	return file, func() { _ = file.Close() }, nil
}

// readCertificateListRows maps the CSV into the row shape the endpoint expects,
// using the header to know which column feeds which value.
func readCertificateListRows(reader io.Reader, kind certificateListKind) ([]map[string]any, error) {
	parser := csv.NewReader(reader)
	parser.FieldsPerRecord = -1
	parser.TrimLeadingSpace = true

	records, err := parser.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("lendo o CSV: %w", err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("o CSV está vazio")
	}

	columns, err := mapCertificateListHeader(records[0], kind)
	if err != nil {
		return nil, err
	}

	rows := make([]map[string]any, 0, len(records)-1)
	for index, record := range records[1:] {
		values := map[string]any{}
		for position, key := range columns {
			if key == "" || position >= len(record) {
				continue
			}
			if value := strings.TrimSpace(record[position]); value != "" {
				values[key] = value
			}
		}
		if len(values) == 0 {
			continue
		}
		rows = append(rows, map[string]any{"index": index, "values": values})
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("o CSV não tem nenhuma linha com dados além do cabeçalho")
	}
	return rows, nil
}

// mapCertificateListHeader returns, per CSV column, which value key it feeds
// (empty for columns the endpoint ignores).
func mapCertificateListHeader(header []string, kind certificateListKind) ([]string, error) {
	columns := make([]string, len(header))
	recognized := 0
	for position, name := range header {
		normalized := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(name, "\ufeff")))
		if key, ok := kind.headerToKey[normalized]; ok {
			columns[position] = key
			recognized++
		}
	}
	if recognized == 0 {
		return nil, fmt.Errorf("o cabeçalho do CSV não tem nenhuma coluna reconhecida (esperado: %s)",
			strings.Join(kind.valueKeys, ", "))
	}
	return columns, nil
}

// decodeImportResponse turns one response into the import view, applying the
// same status-code contract as the dynamic commands: client.Do reports
// transport errors only, so a 401/404/422 arrives as a normal response and has
// to be rejected here — otherwise a Rails error body would be decoded as an
// import with an empty status and the poll would wait for a terminal state that
// never comes.
func decodeImportResponse(
	cmd *cobra.Command,
	imp *auth.Impersonation,
	method string,
	path string,
	response *client.Response,
	view *csvImportView,
) error {
	if response.StatusCode == http.StatusUnauthorized {
		return unauthorizedErr(imp)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if len(response.Body) > 0 {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), strings.TrimSpace(string(response.Body)))
		}
		return fmt.Errorf("%s %s returned %d", method, path, response.StatusCode)
	}
	if err := json.Unmarshal(response.Body, view); err != nil {
		return fmt.Errorf("lendo a resposta: %w", err)
	}
	return nil
}

// sendCertificateListChunks posts every chunk under the same identifier and
// returns the import as the backend last reported it. total_count carries the
// whole file on every chunk, which is what the progress is computed against.
func sendCertificateListChunks(
	cmd *cobra.Command,
	api client.API,
	imp *auth.Impersonation,
	kind certificateListKind,
	certificateID string,
	identifier string,
	filename string,
	rows []map[string]any,
	chunkSize int,
) (csvImportView, error) {
	var accepted csvImportView
	query := url.Values{"id": []string{certificateID}}
	total := len(rows)

	for start := 0; start < total; start += chunkSize {
		end := min(start+chunkSize, total)

		payload := map[string]any{
			kind.bodyRoot: map[string]any{
				"identifier":  identifier,
				"filename":    filename,
				"total_count": total,
				"data":        rows[start:end],
			},
		}

		response, err := api.Do(cmd.Context(), "POST", kind.path, query, payload)
		if err != nil {
			return accepted, fmt.Errorf("enviando as linhas %d-%d de %d: %w", start+1, end, total, err)
		}
		if err := decodeImportResponse(cmd, imp, "POST", kind.path, response, &accepted); err != nil {
			return accepted, fmt.Errorf("enviando as linhas %d-%d de %d: %w", start+1, end, total, err)
		}

		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "enviadas %d de %d linhas\n", end, total)
	}
	return accepted, nil
}

// followCertificateListImport polls the import until it reaches a terminal
// status. The rows run on the within_5_minutes queue, so the interval is
// deliberately slow — a tight loop just burns calls.
func followCertificateListImport(
	cmd *cobra.Command,
	api client.API,
	imp *auth.Impersonation,
	certificateID string,
	identifier string,
	timeout time.Duration,
) (csvImportView, error) {
	var view csvImportView
	path := fmt.Sprintf("/srm_settings/certificates/%s/csv_imports/%s", certificateID, identifier)
	deadline := time.Now().Add(timeout)

	for {
		response, err := api.Get(cmd.Context(), path)
		if err != nil {
			return view, fmt.Errorf("acompanhando o import %s: %w", identifier, err)
		}
		if err := decodeImportResponse(cmd, imp, "GET", path, response, &view); err != nil {
			return view, fmt.Errorf("acompanhando o import %s: %w", identifier, err)
		}
		if view.Status == "" {
			return view, fmt.Errorf("acompanhando o import %s: a resposta não trouxe status", identifier)
		}
		if terminalImportStatuses[view.Status] {
			return view, terminalImportOutcome(view, identifier)
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return view, fmt.Errorf("%w: o import %s ainda está em %s depois de %s; acompanhe com "+
				"`lk settings certificate import show %s %s`",
				errImportUnfinished, identifier, view.Status, timeout, certificateID, identifier)
		}

		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "status %s (%d de %d)\n", view.Status, view.ImportedCount, view.TotalCount)
		if err := sleepContext(cmd.Context(), min(importPollInterval, remaining)); err != nil {
			return view, err
		}
	}
}

// terminalImportOutcome decides the exit status of a finished import. Only
// `completed` is success: `failed` means nothing was applied and `deleted` means
// a `clear` wiped the import mid-flight, and both would be read as success by
// anything chaining commands on the exit code. Rejected rows are not a failure
// here — they come back as `completed` plus a `failed_rows_file_url`.
func terminalImportOutcome(view csvImportView, identifier string) error {
	switch view.Status {
	case "failed":
		return fmt.Errorf("%w: o import %s terminou em failed", errImportUnfinished, identifier)
	case "deleted":
		return fmt.Errorf("%w: o import %s foi marcado como deleted, provavelmente por um `clear` no certificado",
			errImportUnfinished, identifier)
	}
	return nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// newImportIdentifier builds the value that groups the chunks of one file. It is
// random on purpose: the identifier is unique across the whole install, so a
// readable one (`restricoes-2026-08`) collides with another buyer's import and
// the request answers 422.
func newImportIdentifier() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("gerando o identifier do import: %w", err)
	}
	return "lk-" + hex.EncodeToString(buffer), nil
}
