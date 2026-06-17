// Package client is a thin HTTP client for the Linkana REST backend.
//
// The backend has no dedicated API namespace; it serves JSON on its normal
// RESTful routes via Rails content negotiation (format.json). This client
// mirrors that: resource paths get a .json suffix and a Bearer token header.
package client

import (
	"context"
	"net/http"
)

// Response is a minimal HTTP response wrapper.
type Response struct {
	StatusCode int
	Body       []byte
	Header     http.Header
}

// API is the interface for talking to the backend. It is intentionally small
// and grows as commands need more verbs. Commands depend on this interface so
// they can be tested with a mock.
type API interface {
	Get(ctx context.Context, path string) (*Response, error)
}
