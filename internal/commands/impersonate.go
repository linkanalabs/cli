package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/auth"
	"github.com/linkanalabs/cli/internal/client"
	"github.com/linkanalabs/cli/internal/config"
	"github.com/linkanalabs/cli/internal/output"
)

// jsonMarshal is a thin seam over encoding/json.Marshal for testability.
var jsonMarshal = json.Marshal

// impersonationView renders an active impersonation context.
type impersonationView struct {
	*auth.Impersonation
}

// MarshalJSON emits a stable JSON shape that never includes the secret token.
func (v impersonationView) MarshalJSON() ([]byte, error) {
	return jsonMarshalImpersonation(v.Impersonation)
}

// Styled renders the impersonation context as human-readable text.
func (v impersonationView) Styled() string {
	return fmt.Sprintf(
		"impersonando %s\n  buyer:        %s\n  user_id:      %s\n  expira:       %s\n  impersonador: %s\n",
		v.TargetEmail, v.BuyerID, v.TargetUserID, v.ExpiresAt.Format(time.RFC3339), v.ImpersonatorEmail,
	)
}

func newImpersonateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "impersonate <email|user_id>",
		Short: "Impersonar o usuário @linkana de um buyer (SRM)",
		Long: "Cunha um Access Token no buyer+user de destino e passa a agir como ele.\n" +
			"O estado é pegajoso: ao expirar, comandos falham até `lk impersonate stop` ou re-impersonar.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return runImpersonateStart(cmd, args[0])
		},
	}
	cmd.Flags().Duration("ttl", 0, "tempo de vida do token (ex: 24h); vazio usa o default do backend")
	cmd.AddCommand(newImpersonateStopCmd())
	cmd.AddCommand(newImpersonateStatusCmd())
	return cmd
}

func runImpersonateStart(cmd *cobra.Command, ref string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	token, _, err := authLoad(cfg.BaseURL)
	if err != nil {
		return err
	}
	if token == "" {
		return fmt.Errorf("not authenticated; run `lk auth login`")
	}

	// Best-effort revoke any existing impersonation before minting a new one.
	if prior, _ := auth.LoadImpersonation(cfg.BaseURL); prior != nil {
		priorAPI := newAPI(cfg.BaseURL, prior.Token)
		if revokeErr := priorAPI.StopImpersonation(cmd.Context()); revokeErr != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
				"aviso: revogação do token anterior falhou (%v); seguindo com nova impersonação\n", revokeErr)
		}
	}

	api := newAPI(cfg.BaseURL, token)

	ttl, _ := cmd.Flags().GetDuration("ttl")
	imp, err := api.StartImpersonation(cmd.Context(), ref, ttl)
	if err != nil {
		if errors.Is(err, client.ErrUnauthorized) {
			return fmt.Errorf("token rejected (401); run `lk auth login` to re-authenticate")
		}
		return err
	}

	impersonator := ""
	if id, idErr := api.GetIdentity(cmd.Context()); idErr == nil {
		impersonator = id.Email
	} else {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "aviso: não foi possível obter o email do impersonador (%v); seguindo sem ele\n", idErr)
	}

	ctx := auth.Impersonation{
		Token:             imp.Token,
		TargetEmail:       imp.Identity.Email,
		TargetUserID:      imp.Identity.UserID,
		BuyerID:           imp.Identity.BuyerID,
		ImpersonatorEmail: impersonator,
		ExpiresAt:         imp.ExpiresAt,
	}
	if err := auth.SaveImpersonation(cfg.BaseURL, ctx); err != nil {
		return fmt.Errorf("saving impersonation context: %w", err)
	}
	return output.Render(cmd.OutOrStdout(), formatFlag(cmd), impersonationView{Impersonation: &ctx})
}

func newImpersonateStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Encerrar a impersonação ativa",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			imp, err := auth.LoadImpersonation(cfg.BaseURL)
			if err != nil {
				return err
			}
			if imp == nil {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "nenhuma impersonação ativa")
				return nil
			}
			// Best-effort revoke using the impersonation token itself.
			api := newAPI(cfg.BaseURL, imp.Token)
			if stopErr := api.StopImpersonation(cmd.Context()); stopErr != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "aviso: revogação remota falhou (%v); limpando estado local mesmo assim\n", stopErr)
			}
			if err := auth.DeleteImpersonation(cfg.BaseURL); err != nil {
				return fmt.Errorf("clearing impersonation context: %w", err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "impersonação de %s encerrada\n", imp.TargetEmail)
			return nil
		},
	}
}

func newImpersonateStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Mostrar a impersonação ativa",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			imp, err := auth.LoadImpersonation(cfg.BaseURL)
			if err != nil {
				return err
			}
			if imp == nil {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "nenhuma impersonação ativa")
				return nil
			}
			note := ""
			if imp.Expired(timeNow()) {
				note = " (EXPIRADA — rode `lk impersonate stop` ou re-impersonar)"
			}
			if formatFlag(cmd) == output.FormatJSON {
				return output.Render(cmd.OutOrStdout(), output.FormatJSON, impersonationView{Impersonation: imp})
			}
			out := impersonationView{Impersonation: imp}.Styled()
			_, _ = fmt.Fprint(cmd.OutOrStdout(), strings.TrimRight(out, "\n")+note+"\n")
			return nil
		},
	}
}

// jsonMarshalImpersonation emits a stable JSON shape (without the secret token).
func jsonMarshalImpersonation(i *auth.Impersonation) ([]byte, error) {
	type public struct {
		TargetEmail       string    `json:"target_email"`
		TargetUserID      string    `json:"target_user_id"`
		BuyerID           string    `json:"buyer_id"`
		ImpersonatorEmail string    `json:"impersonator_email"`
		ExpiresAt         time.Time `json:"expires_at"`
	}
	return jsonMarshal(public{
		TargetEmail:       i.TargetEmail,
		TargetUserID:      i.TargetUserID,
		BuyerID:           i.BuyerID,
		ImpersonatorEmail: i.ImpersonatorEmail,
		ExpiresAt:         i.ExpiresAt,
	})
}
