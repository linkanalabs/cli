package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Tag is a label attached to a supplier. It matches the Rails jbuilder contract.
type Tag struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// Supplier is a supplier resource as returned by the SRM endpoints. It matches
// the CLI↔Rails contract exactly.
type Supplier struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Identifier  string `json:"identifier"`
	LegalEntity string `json:"legal_entity"`
	State       string `json:"state"`
	Tags        []Tag  `json:"tags"`
}

// ListSuppliers fetches the suppliers from GET /srm/suppliers.json. A 401 is
// reported as ErrUnauthorized so callers can hint the user to re-login.
func (c *Client) ListSuppliers(ctx context.Context) ([]Supplier, error) {
	resp, err := c.Get(ctx, "/srm/suppliers")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("suppliers request returned %d", resp.StatusCode)
	}

	var suppliers []Supplier
	if err := json.Unmarshal(resp.Body, &suppliers); err != nil {
		return nil, fmt.Errorf("decoding suppliers: %w", err)
	}
	return suppliers, nil
}

// GetSupplier fetches a single supplier from GET /srm/suppliers/<id>/panel.json.
// A 401 is reported as ErrUnauthorized so callers can hint the user to re-login.
func (c *Client) GetSupplier(ctx context.Context, id string) (*Supplier, error) {
	resp, err := c.Get(ctx, "/srm/suppliers/"+id+"/panel")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("supplier request returned %d", resp.StatusCode)
	}

	var s Supplier
	if err := json.Unmarshal(resp.Body, &s); err != nil {
		return nil, fmt.Errorf("decoding supplier: %w", err)
	}
	return &s, nil
}
