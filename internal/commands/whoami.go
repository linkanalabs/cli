package commands

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/auth"
	"github.com/linkanalabs/cli/internal/client"
	"github.com/linkanalabs/cli/internal/config"
	"github.com/linkanalabs/cli/internal/output"
)

// errNoToken signals that no token is configured for the active backend.
var errNoToken = errors.New("no token configured")

// errImpersonationExpired signals a stored-but-expired impersonation context.
// Resolution must NOT fall back to the original token in this state.
var errImpersonationExpired = errors.New("impersonation expired")

// timeNow is a seam so tests can control expiry evaluation.
var timeNow = time.Now

// newAPI is a seam so tests/commands can substitute the backend client.
var newAPI = func(baseURL, token string) client.API {
	c := client.New(baseURL)
	c.Token = token
	return c
}

// authedClient resolves the active credential for the configured backend.
//
//   - impersonation context present & not expired → use the impersonation token.
//   - impersonation context present & expired      → errImpersonationExpired
//     (sticky; never falls back to the original token).
//   - no impersonation context                     → use the original token.
func authedClient() (client.API, string, *auth.Impersonation, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, "", nil, err
	}
	imp, err := auth.LoadImpersonation(cfg.BaseURL)
	if err != nil {
		return nil, cfg.BaseURL, nil, err
	}
	if imp != nil {
		if imp.Expired(timeNow()) {
			return nil, cfg.BaseURL, imp, errImpersonationExpired
		}
		return newAPI(cfg.BaseURL, imp.Token), cfg.BaseURL, imp, nil
	}
	token, _, err := authLoad(cfg.BaseURL)
	if err != nil {
		return nil, cfg.BaseURL, nil, err
	}
	if token == "" {
		return nil, cfg.BaseURL, nil, errNoToken
	}
	return newAPI(cfg.BaseURL, token), cfg.BaseURL, nil, nil
}

// resolveAPI wraps authedClient and maps known errors to user-facing messages.
func resolveAPI() (client.API, *auth.Impersonation, error) {
	api, _, imp, err := authedClient()
	if err == nil {
		return api, imp, nil
	}
	switch {
	case errors.Is(err, errNoToken):
		return nil, nil, fmt.Errorf("not authenticated; run `lk auth login`")
	case errors.Is(err, errImpersonationExpired):
		return nil, imp, impersonationExpiredErr(imp)
	default:
		return nil, imp, err
	}
}

// impersonationExpiredErr renders the sticky-expiry guidance.
func impersonationExpiredErr(imp *auth.Impersonation) error {
	return fmt.Errorf(
		"impersonação de %s (buyer %s) expirou em %s.\n"+
			"rode `lk impersonate %s` pra renovar, ou `lk impersonate stop` pra voltar ao usuário original",
		imp.TargetEmail, imp.BuyerID, imp.ExpiresAt.Format(time.RFC3339), imp.TargetEmail,
	)
}

// unauthorizedErr renders a 401 message, aware of an active impersonation.
func unauthorizedErr(imp *auth.Impersonation) error {
	if imp != nil {
		return fmt.Errorf(
			"token de impersonação rejeitado (expirou ou foi revogado no servidor).\n"+
				"você está impersonando %s (buyer %s).\n"+
				"  • lk impersonate stop      → voltar ao usuário original\n"+
				"  • lk impersonate %s        → impersonar de novo (renova o token)",
			imp.TargetEmail, imp.BuyerID, imp.TargetEmail,
		)
	}
	return fmt.Errorf("token rejected (401); run `lk auth login` to re-authenticate")
}

// identityView wraps an identity for human-friendly styled output.
type identityView struct {
	*client.Identity
}

// Styled renders the identity as text.
func (v identityView) Styled() string {
	buyer := "(none)"
	if v.BuyerID != nil {
		buyer = *v.BuyerID
	}
	return fmt.Sprintf(
		"%s <%s>\n  id:      %s\n  role:    %s\n  buyer:   %s\n  staff:   %t\n",
		v.Name, v.Email, v.ID, v.Role, buyer, v.IsStaff,
	)
}

func newWhoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the authenticated identity",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			api, imp, err := resolveAPI()
			if err != nil {
				return err
			}
			id, err := api.GetIdentity(cmd.Context())
			if err != nil {
				if errors.Is(err, client.ErrUnauthorized) {
					return unauthorizedErr(imp)
				}
				return err
			}
			return output.Render(cmd.OutOrStdout(), formatFlag(cmd), identityView{Identity: id})
		},
	}
}
