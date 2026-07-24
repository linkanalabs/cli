package commands

import (
	"encoding/json"
	"errors"

	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/client"
	"github.com/linkanalabs/cli/internal/output"
)

// supplierListView wraps a supplier slice so JSON renders as a bare array
// (the contract) even when the slice is nil. Styled output is the generic
// renderer's, same as dynamic commands.
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
			api, imp, _, err := resolveAPI()
			if err != nil {
				return err
			}
			suppliers, err := api.ListSuppliers(cmd.Context())
			if err != nil {
				if errors.Is(err, client.ErrUnauthorized) {
					return unauthorizedErr(imp)
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
			api, imp, _, err := resolveAPI()
			if err != nil {
				return err
			}
			s, err := api.GetSupplier(cmd.Context(), args[0])
			if err != nil {
				if errors.Is(err, client.ErrUnauthorized) {
					return unauthorizedErr(imp)
				}
				return err
			}
			return output.Render(cmd.OutOrStdout(), formatFlag(cmd), s)
		},
	}
}
