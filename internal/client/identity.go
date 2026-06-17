package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ErrUnauthorized is returned when the backend rejects the token (HTTP 401).
var ErrUnauthorized = errors.New("unauthorized: token missing or invalid")

// Identity is the authenticated principal, as returned by GET /my/identity.json.
// It matches the CLI↔Rails contract exactly.
type Identity struct {
	ID      string  `json:"id"`
	Email   string  `json:"email"`
	Name    string  `json:"name"`
	Role    string  `json:"role"`
	BuyerID *string `json:"buyer_id"`
	IsStaff bool    `json:"is_staff"`
}

// GetIdentity fetches the authenticated identity from /my/identity.json. A 401
// is reported as ErrUnauthorized so callers can hint the user to re-login.
func (c *Client) GetIdentity(ctx context.Context) (*Identity, error) {
	resp, err := c.Get(ctx, "/my/identity")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("identity request returned %d", resp.StatusCode)
	}

	var id Identity
	if err := json.Unmarshal(resp.Body, &id); err != nil {
		return nil, fmt.Errorf("decoding identity: %w", err)
	}
	return &id, nil
}
