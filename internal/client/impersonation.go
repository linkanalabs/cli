package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Impersonation is the response from POST /impersonation.json.
type Impersonation struct {
	Token     string                `json:"token"`
	Identity  ImpersonationIdentity `json:"identity"`
	ExpiresAt time.Time             `json:"expires_at"`
}

// ImpersonationIdentity is the impersonated principal (the target support user).
type ImpersonationIdentity struct {
	UserID  string `json:"user_id"`
	Email   string `json:"email"`
	BuyerID string `json:"buyer_id"`
}

type startImpersonationRequest struct {
	Target     string `json:"target"`
	TTLSeconds int    `json:"ttl_seconds,omitempty"`
}

// StartImpersonation mints an impersonation Access Token for userRef (email or
// uuid). A zero ttl lets the backend apply its default.
func (c *Client) StartImpersonation(ctx context.Context, userRef string, ttl time.Duration) (*Impersonation, error) {
	body := startImpersonationRequest{Target: userRef, TTLSeconds: int(ttl.Seconds())}
	resp, err := c.Post(ctx, "/impersonation", body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("impersonation request returned %d: %s", resp.StatusCode, serverError(resp.Body))
	}

	var imp Impersonation
	if err := json.Unmarshal(resp.Body, &imp); err != nil {
		return nil, fmt.Errorf("decoding impersonation: %w", err)
	}
	return &imp, nil
}

// StopImpersonation revokes the impersonation token in use. A 401 (token already
// expired/revoked server-side) is treated as success so the caller can always
// clear local state.
func (c *Client) StopImpersonation(ctx context.Context) error {
	resp, err := c.Delete(ctx, "/impersonation")
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("stop impersonation returned %d", resp.StatusCode)
	}
	return nil
}

// serverError extracts a JSON {"error": "..."} message, falling back to the raw body.
func serverError(body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return e.Error
	}
	return string(body)
}
