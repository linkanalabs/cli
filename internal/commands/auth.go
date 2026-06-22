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

// statusResult is the payload for `auth status`. It never carries the secret.
type statusResult struct {
	Authenticated bool   `json:"authenticated"`
	BaseURL       string `json:"base_url"`
	Source        string `json:"source"`
}

// Styled renders the status result as text.
func (r statusResult) Styled() string {
	if !r.Authenticated {
		return fmt.Sprintf("Not authenticated for %s\n", r.BaseURL)
	}
	return fmt.Sprintf("Authenticated for %s (source: %s)\n", r.BaseURL, r.Source)
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

func newAuthLoginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Store a Personal Access Token for the active backend",
		Args:  cobra.NoArgs,
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

	token, err := resolveLoginToken(cmd, flagToken)
	if err != nil {
		return err
	}
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, tokenPrefix) {
		return fmt.Errorf("token does not look like a Linkana PAT (expected %s...)", tokenPrefix)
	}

	if err := authSave(baseURL, token); err != nil {
		return fmt.Errorf("saving token: %w", err)
	}
	return output.Render(cmd.OutOrStdout(), formatFlag(cmd), loginResult{Status: "saved", BaseURL: baseURL})
}

// resolveLoginToken returns the token from the flag, then LK_TOKEN, then an
// interactive prompt on stdin.
func resolveLoginToken(cmd *cobra.Command, flagToken string) (string, error) {
	if flagToken != "" {
		return flagToken, nil
	}
	if env := getenv(auth.EnvToken); env != "" {
		return env, nil
	}
	return promptToken(cmd.InOrStdin(), cmd.ErrOrStderr())
}

func promptToken(in io.Reader, prompt io.Writer) (string, error) {
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
			res := statusResult{
				Authenticated: token != "",
				BaseURL:       baseURL,
				Source:        string(src),
			}
			if err := output.Render(cmd.OutOrStdout(), formatFlag(cmd), res); err != nil {
				return err
			}
			imp, _ := auth.LoadImpersonation(baseURL)
			if imp != nil {
				state := "ativa"
				if imp.Expired(timeNow()) {
					state = "EXPIRADA"
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(),
					"impersonação (%s): %s (buyer %s, expira %s; por %s)\n",
					state, imp.TargetEmail, imp.BuyerID, imp.ExpiresAt.Format(time.RFC3339), imp.ImpersonatorEmail)
			}
			return nil
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
