package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
)

// Do performs a generic request against the backend, routed through the
// central dispatcher (read/write gate, Bearer token, .json suffix). The query
// is encoded after the path so ensureJSON keeps the suffix before the query
// string (path.json?query). A non-nil payload is JSON-encoded as the body.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, payload any) (*Response, error) {
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encoding request body: %w", err)
		}
		body = bytes.NewReader(b)
	}
	return c.do(ctx, method, path, body)
}
