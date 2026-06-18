package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/client"
	"github.com/linkanalabs/cli/internal/output"
)

// supplierListView wraps a supplier slice. JSON renders as a bare array (the
// contract); styled renders one line per supplier.
type supplierListView struct {
	Suppliers []client.Supplier
}

// MarshalJSON renders the list as a bare JSON array, preserving the contract.
func (v supplierListView) MarshalJSON() ([]byte, error) {
	suppliers := v.Suppliers
	if suppliers == nil {
		suppliers = []client.Supplier{}
	}
	return json.Marshal(suppliers)
}

// Styled renders one line per supplier with id/name/identifier/state.
func (v supplierListView) Styled() string {
	if len(v.Suppliers) == 0 {
		return "No suppliers found.\n"
	}
	var b strings.Builder
	for _, s := range v.Suppliers {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", s.ID, s.Name, s.Identifier, s.State)
	}
	return b.String()
}

// supplierView wraps a single supplier for human-friendly styled output.
type supplierView struct {
	*client.Supplier
}

// Styled renders the supplier as a key/value block.
func (v supplierView) Styled() string {
	tags := "(none)"
	if len(v.Tags) > 0 {
		names := make([]string, len(v.Tags))
		for i, t := range v.Tags {
			names[i] = t.DisplayName
		}
		tags = strings.Join(names, ", ")
	}
	return fmt.Sprintf(
		"%s\n  id:           %s\n  identifier:   %s\n  legal_entity: %s\n  state:        %s\n  tags:         %s\n",
		v.Name, v.ID, v.Identifier, v.LegalEntity, v.State, tags,
	)
}

func newSupplierCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "supplier",
		Short: "Inspect suppliers (SRM)",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newSupplierListCmd())
	cmd.AddCommand(newSupplierShowCmd())
	return cmd
}

func newSupplierListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List suppliers (GET /srm/suppliers)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			api, _, err := authedClient()
			if err != nil {
				if errors.Is(err, errNoToken) {
					return fmt.Errorf("not authenticated; run `lk auth login`")
				}
				return err
			}
			suppliers, err := api.ListSuppliers(cmd.Context())
			if err != nil {
				if errors.Is(err, client.ErrUnauthorized) {
					return fmt.Errorf("token rejected (401); run `lk auth login` to re-authenticate")
				}
				return err
			}
			return output.Render(cmd.OutOrStdout(), formatFlag(cmd), supplierListView{Suppliers: suppliers})
		},
	}
}

func newSupplierShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show a single supplier (GET /srm/suppliers/<id>/panel)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			api, _, err := authedClient()
			if err != nil {
				if errors.Is(err, errNoToken) {
					return fmt.Errorf("not authenticated; run `lk auth login`")
				}
				return err
			}
			s, err := api.GetSupplier(cmd.Context(), args[0])
			if err != nil {
				if errors.Is(err, client.ErrUnauthorized) {
					return fmt.Errorf("token rejected (401); run `lk auth login` to re-authenticate")
				}
				return err
			}
			return output.Render(cmd.OutOrStdout(), formatFlag(cmd), supplierView{Supplier: s})
		},
	}
}
