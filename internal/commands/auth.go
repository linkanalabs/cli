package commands

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/auth"
	"github.com/linkanalabs/cli/internal/config"
	"github.com/linkanalabs/cli/internal/mode"
	"github.com/linkanalabs/cli/internal/output"
)

// getenv is a seam over os.Getenv.
var getenv = os.Getenv

// tokenPrefix is the required prefix of a Linkana PAT (lkn_<short>_<long>).
const tokenPrefix = "lkn_"

// auth storage seams so command error branches stay testable.
var (
	authLoad   = auth.Load
	authSave   = auth.Save
	authDelete = auth.Delete
)

// activeBaseURL resolves the base URL the way the rest of the CLI does.
func activeBaseURL() (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	return cfg.BaseURL, nil
}

// loginResult is the payload for `auth login`.
type loginResult struct {
	Status  string `json:"status"`
	BaseURL string `json:"base_url"`
}

// Styled renders the login result as text.
func (r loginResult) Styled() string {
	return fmt.Sprintf("Token saved for %s\n", r.BaseURL)
}

// statusImpersonation is the impersonation block inside statusResult JSON.
// It never carries the secret token.
type statusImpersonation struct {
	TargetEmail       string    `json:"target_email"`
	BuyerID           string    `json:"buyer_id"`
	ExpiresAt         time.Time `json:"expires_at"`
	ImpersonatorEmail string    `json:"impersonator_email"`
	Expired           bool      `json:"expired"`
}

// statusResult is the payload for `auth status`. It never carries the secret.
type statusResult struct {
	Authenticated bool                 `json:"authenticated"`
	BaseURL       string               `json:"base_url"`
	Source        string               `json:"source"`
	Mode          mode.Mode            `json:"mode"`
	Impersonation *statusImpersonation `json:"impersonation,omitempty"`
}

// Styled renders the status result as text.
func (r statusResult) Styled() string {
	var b strings.Builder
	if r.Authenticated {
		fmt.Fprintf(&b, "Authenticated for %s (source: %s, mode: %s)\n", r.BaseURL, r.Source, r.Mode)
	} else {
		fmt.Fprintf(&b, "Not authenticated for %s\n", r.BaseURL)
	}
	if imp := r.Impersonation; imp != nil {
		state := "ativa"
		if imp.Expired {
			state = "EXPIRADA"
		}
		fmt.Fprintf(&b, "impersonação (%s): %s (buyer %s, expira %s; por %s)\n",
			state, imp.TargetEmail, imp.BuyerID, imp.ExpiresAt.Format(time.RFC3339), imp.ImpersonatorEmail)
	}
	return b.String()
}

// logoutResult is the payload for `auth logout`.
type logoutResult struct {
	Status  string `json:"status"`
	BaseURL string `json:"base_url"`
}

// Styled renders the logout result as text.
func (r logoutResult) Styled() string {
	return fmt.Sprintf("Logged out of %s\n", r.BaseURL)
}

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication for the Linkana backend",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newAuthLoginCmd())
	cmd.AddCommand(newAuthStatusCmd())
	cmd.AddCommand(newAuthLogoutCmd())
	return cmd
}

// accessTokensURL is the page on the active backend where PATs are generated.
func accessTokensURL(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/srm_settings/access_tokens"
}

func newAuthLoginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Store a Personal Access Token for the active backend",
		Long: `Guarda um Personal Access Token (PAT) para o backend ativo.

O token é lido de --token, depois da variável de ambiente LK_TOKEN e, por
último, de um prompt interativo.

Gere um PAT em <base_url>/srm_settings/access_tokens (configurações do SRM,
menu "Tokens de acesso" — apenas usuários Linkana): clique em "Novo token",
confirme em "Criar token" e copie o segredo na modal "Copie seu token agora".
O segredo é exibido uma única vez. Tokens têm o formato lkn_<short>_<long>.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			flagToken, _ := cmd.Flags().GetString("token")
			return runAuthLogin(cmd, flagToken)
		},
	}
	cmd.Flags().String("token", "", "Personal Access Token (lkn_...); read from LK_TOKEN or prompted if omitted")
	return cmd
}

func runAuthLogin(cmd *cobra.Command, flagToken string) error {
	baseURL, err := activeBaseURL()
	if err != nil {
		return err
	}

	token, err := resolveLoginToken(cmd, flagToken, baseURL)
	if err != nil {
		return err
	}
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, tokenPrefix) {
		return fmt.Errorf("o token não parece um PAT da Linkana (esperado %s...); gere um em %s", tokenPrefix, accessTokensURL(baseURL))
	}

	if err := authSave(baseURL, token); err != nil {
		return fmt.Errorf("saving token: %w", err)
	}
	return output.Render(cmd.OutOrStdout(), formatFlag(cmd), loginResult{Status: "saved", BaseURL: baseURL})
}

// resolveLoginToken returns the token from the flag, then LK_TOKEN, then an
// interactive prompt on stdin.
func resolveLoginToken(cmd *cobra.Command, flagToken, baseURL string) (string, error) {
	if flagToken != "" {
		return flagToken, nil
	}
	if env := getenv(auth.EnvToken); env != "" {
		return env, nil
	}
	return promptToken(cmd.InOrStdin(), cmd.ErrOrStderr(), baseURL)
}

func promptToken(in io.Reader, prompt io.Writer, baseURL string) (string, error) {
	_, _ = fmt.Fprintf(prompt, `Ainda sem token? Gere um em:

  %s

Clique em "Novo token", confirme em "Criar token" e copie o segredo na modal
"Copie seu token agora" — ele é exibido uma única vez. Depois volte aqui e
cole abaixo.

`, accessTokensURL(baseURL))
	_, _ = fmt.Fprint(prompt, "Token: ")
	sc := bufio.NewScanner(in)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return "", fmt.Errorf("reading token from stdin: %w", err)
		}
		return "", fmt.Errorf("no token provided")
	}
	return sc.Text(), nil
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether a token is stored for the active backend",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			baseURL, err := activeBaseURL()
			if err != nil {
				return err
			}
			token, src, err := authLoad(baseURL)
			if err != nil {
				return err
			}
			m, err := mode.Load(baseURL)
			if err != nil {
				return fmt.Errorf("loading mode: %w", err)
			}
			res := statusResult{
				Authenticated: token != "",
				BaseURL:       baseURL,
				Source:        string(src),
				Mode:          m,
			}

			// Load impersonation context — warn on error but do not fail.
			imp, impErr := auth.LoadImpersonation(baseURL)
			if impErr != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
					"aviso: não foi possível ler o contexto de impersonação: %v\n", impErr)
			}
			if imp != nil {
				expired := imp.Expired(timeNow())
				res.Impersonation = &statusImpersonation{
					TargetEmail:       imp.TargetEmail,
					BuyerID:           imp.BuyerID,
					ExpiresAt:         imp.ExpiresAt,
					ImpersonatorEmail: imp.ImpersonatorEmail,
					Expired:           expired,
				}
				// Impersonation context takes precedence over the base token
				// (see CLAUDE.md). While active, auth status reflects the
				// impersonation: authenticated only while not expired. An
				// expired context is sticky — a hard error with no fallback to
				// the original token — so it reads as not authenticated.
				res.Authenticated = !expired
			}

			// Render handles format resolution (auto → JSON when piped). The
			// impersonation block lives inside the view, so both JSON and styled
			// stay consistent — no out-of-band append that could corrupt JSON.
			return output.Render(cmd.OutOrStdout(), formatFlag(cmd), res)
		},
	}
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Delete the stored token for the active backend",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			baseURL, err := activeBaseURL()
			if err != nil {
				return err
			}
			if err := authDelete(baseURL); err != nil {
				return fmt.Errorf("deleting token: %w", err)
			}
			return output.Render(cmd.OutOrStdout(), formatFlag(cmd), logoutResult{Status: "logged out", BaseURL: baseURL})
		},
	}
}
