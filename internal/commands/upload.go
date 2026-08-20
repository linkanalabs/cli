package commands

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // Active Storage requires an MD5 checksum for direct uploads.
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/auth"
	"github.com/linkanalabs/cli/internal/client"
	"github.com/linkanalabs/cli/internal/output"
)

const directUploadsPath = "/rails/active_storage/direct_uploads"

// storagePut performs the second leg of a direct upload (the raw PUT of the
// bytes to the storage service URL). It is a seam so tests can intercept it —
// the URL is absolute and outside the API client (no Bearer, exact headers).
var storagePut = func(req *http.Request) (*http.Response, error) {
	return http.DefaultClient.Do(req)
}

// directUploadBlob is the direct_uploads#create response: the signed_id the
// backend expects in attachment params, plus where and how to PUT the bytes.
type directUploadBlob struct {
	SignedID     string `json:"signed_id"`
	Filename     string `json:"filename"`
	ByteSize     int64  `json:"byte_size"`
	ContentType  string `json:"content_type"`
	DirectUpload struct {
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	} `json:"direct_upload"`
}

func newUploadCmd() *cobra.Command {
	var filePath string
	var fromStdin bool
	var filename string
	var contentType string

	cmd := &cobra.Command{
		Use:   "upload",
		Short: "Sobe um arquivo via direct upload e imprime o signed_id",
		Long: "Sobe um arquivo para o storage da Linkana via direct upload (Active Storage) " +
			"e imprime o signed_id resultante. O signed_id é o valor que os comandos de update " +
			"esperam em params de anexo — ex.: lk settings company update --logo <signed_id>.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUpload(cmd, filePath, fromStdin, filename, contentType)
		},
	}
	cmd.Flags().StringVar(&filePath, "file", "", "caminho do arquivo a subir")
	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "lê o conteúdo do stdin (exige --filename)")
	cmd.Flags().StringVar(&filename, "filename", "", "nome do arquivo no storage (default: basename de --file)")
	cmd.Flags().StringVar(&contentType, "content-type", "", "content type do arquivo (default: detectado do conteúdo)")
	return cmd
}

func runUpload(cmd *cobra.Command, filePath string, fromStdin bool, filename, contentType string) error {
	data, filename, err := readUploadSource(cmd, filePath, fromStdin, filename)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return fmt.Errorf("o arquivo está vazio")
	}
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}

	api, imp, err := resolveAPI()
	if err != nil {
		return err
	}

	blob, err := createDirectUpload(cmd, api, imp, data, filename, contentType)
	if err != nil {
		return err
	}
	if err := putUploadBytes(cmd.Context(), blob, data); err != nil {
		return err
	}

	return output.Render(cmd.OutOrStdout(), formatFlag(cmd), map[string]any{
		"signed_id":    blob.SignedID,
		"filename":     blob.Filename,
		"byte_size":    blob.ByteSize,
		"content_type": blob.ContentType,
	})
}

// readUploadSource resolves --file / --stdin into the full content plus the
// filename the blob is stored under, refusing both or neither.
func readUploadSource(cmd *cobra.Command, filePath string, fromStdin bool, filename string) ([]byte, string, error) {
	switch {
	case filePath != "" && fromStdin:
		return nil, "", fmt.Errorf("use --file ou --stdin, não os dois")
	case filePath == "" && !fromStdin:
		return nil, "", fmt.Errorf("informe --file <caminho> ou --stdin")
	case fromStdin:
		if filename == "" {
			return nil, "", fmt.Errorf("--stdin exige --filename <nome>")
		}
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, "", fmt.Errorf("lendo o stdin: %w", err)
		}
		return data, filename, nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, "", fmt.Errorf("lendo %s: %w", filePath, err)
	}
	if filename == "" {
		filename = filepath.Base(filePath)
	}
	return data, filename, nil
}

// createDirectUpload registers the blob metadata on the backend and returns
// the signed_id plus the storage URL/headers for the byte PUT.
func createDirectUpload(
	cmd *cobra.Command,
	api client.API,
	imp *auth.Impersonation,
	data []byte,
	filename string,
	contentType string,
) (*directUploadBlob, error) {
	checksum := md5.Sum(data) //nolint:gosec // Active Storage's direct upload contract.
	payload := map[string]any{
		"blob": map[string]any{
			"filename":     filename,
			"byte_size":    len(data),
			"checksum":     base64.StdEncoding.EncodeToString(checksum[:]),
			"content_type": contentType,
		},
	}

	response, err := api.Do(cmd.Context(), "POST", directUploadsPath, nil, payload)
	if err != nil {
		return nil, err
	}
	if response.StatusCode == http.StatusUnauthorized {
		return nil, unauthorizedErr(imp)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if len(response.Body) > 0 {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), strings.TrimSpace(string(response.Body)))
		}
		return nil, fmt.Errorf("POST %s returned %d", directUploadsPath, response.StatusCode)
	}

	blob := &directUploadBlob{}
	if err := json.Unmarshal(response.Body, blob); err != nil {
		return nil, fmt.Errorf("lendo a resposta do direct upload: %w", err)
	}
	if blob.SignedID == "" || blob.DirectUpload.URL == "" {
		return nil, fmt.Errorf("resposta do direct upload sem signed_id ou URL de upload")
	}
	return blob, nil
}

// putUploadBytes performs the storage PUT with exactly the headers the backend
// signed — adding or changing one invalidates the signature on S3-like services.
func putUploadBytes(ctx context.Context, blob *directUploadBlob, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, blob.DirectUpload.URL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("montando o PUT do upload: %w", err)
	}
	for key, value := range blob.DirectUpload.Headers {
		req.Header.Set(key, value)
	}

	resp, err := storagePut(req)
	if err != nil {
		return fmt.Errorf("enviando os bytes do upload: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if len(body) > 0 {
			return fmt.Errorf("PUT do upload returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return fmt.Errorf("PUT do upload returned %d", resp.StatusCode)
	}
	return nil
}
