package auth

import (
	"encoding/json"
	"fmt"
	"time"
)

// Impersonation is the locally-persisted impersonation context for an origin.
// It lives alongside (not replacing) the original token; while present, callers
// must use Token instead of the original credential.
type Impersonation struct {
	Token             string    `json:"token"`
	TargetEmail       string    `json:"target_email"`
	TargetUserID      string    `json:"target_user_id"`
	BuyerID           string    `json:"buyer_id"`
	ImpersonatorEmail string    `json:"impersonator_email"`
	ExpiresAt         time.Time `json:"expires_at"`
}

// Expired reports whether the context is past its expiry at the given instant.
func (i Impersonation) Expired(now time.Time) bool {
	return now.After(i.ExpiresAt)
}

// impersonationOrigin namespaces the impersonation blob so it never collides
// with the origin's original token in the same keychain/file store.
func impersonationOrigin(origin string) string {
	return origin + "|impersonation"
}

// SaveImpersonation persists the impersonation context for origin.
func SaveImpersonation(origin string, imp Impersonation) error {
	blob, err := json.Marshal(imp)
	if err != nil {
		return fmt.Errorf("encoding impersonation: %w", err)
	}
	return Save(impersonationOrigin(origin), string(blob))
}

// LoadImpersonation returns the stored impersonation context, or nil when none.
func LoadImpersonation(origin string) (*Impersonation, error) {
	blob, src, err := Load(impersonationOrigin(origin))
	if err != nil {
		return nil, err
	}
	if blob == "" || src == SourceEnv {
		return nil, nil
	}
	var imp Impersonation
	if err := json.Unmarshal([]byte(blob), &imp); err != nil {
		return nil, fmt.Errorf("decoding impersonation: %w", err)
	}
	return &imp, nil
}

// DeleteImpersonation removes the impersonation context for origin.
func DeleteImpersonation(origin string) error {
	return Delete(impersonationOrigin(origin))
}
