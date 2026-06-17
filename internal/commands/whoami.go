package commands

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/client"
	"github.com/linkanalabs/cli/internal/config"
	"github.com/linkanalabs/cli/internal/output"
)

// errNoToken signals that no token is configured for the active backend.
var errNoToken = errors.New("no token configured")

// newAPI is a seam so tests/commands can substitute the backend client.
var newAPI = func(baseURL, token string) client.API {
	c := client.New(baseURL)
	c.Token = token
	return c
}

// authedClient resolves base URL + stored token (env LK_TOKEN overrides) and
// returns a configured API client. It returns errNoToken when no token exists.
func authedClient() (client.API, string, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, "", err
	}
	token, _, err := authLoad(cfg.BaseURL)
	if err != nil {
		return nil, cfg.BaseURL, err
	}
	if token == "" {
		return nil, cfg.BaseURL, errNoToken
	}
	return newAPI(cfg.BaseURL, token), cfg.BaseURL, nil
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
			api, _, err := authedClient()
			if err != nil {
				if errors.Is(err, errNoToken) {
					return fmt.Errorf("not authenticated; run `lk auth login`")
				}
				return err
			}
			id, err := api.GetIdentity(cmd.Context())
			if err != nil {
				if errors.Is(err, client.ErrUnauthorized) {
					return fmt.Errorf("token rejected (401); run `lk auth login` to re-authenticate")
				}
				return err
			}
			return output.Render(cmd.OutOrStdout(), formatFlag(cmd), identityView{Identity: id})
		},
	}
}
