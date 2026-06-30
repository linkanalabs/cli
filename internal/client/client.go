package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/linkanalabs/cli/internal/mode"
)

// ErrReadOnly is returned for a mutating request while the origin is in read mode.
var ErrReadOnly = errors.New("CLI is in read mode")

const defaultTimeout = 30 * time.Second

// Client is an HTTP client for the Linkana backend.
type Client struct {
	BaseURL    string
	Token      string
	Mode       mode.Mode
	HTTPClient *http.Client
}

// Ensure Client satisfies the API interface.
var _ API = (*Client)(nil)

// New creates a Client for the given base URL.
func New(baseURL string) *Client {
	return &Client{
		BaseURL:    strings.TrimSuffix(baseURL, "/"),
		HTTPClient: &http.Client{Timeout: defaultTimeout},
	}
}

// buildURL joins the base URL and path, ensuring a leading slash and a .json
// suffix on the path (Rails content negotiation). Absolute URLs pass through.
func (c *Client) buildURL(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.BaseURL + ensureJSON(path)
}

// ensureJSON appends .json to the path (before any query string) when absent.
func ensureJSON(path string) string {
	query := ""
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path, query = path[:i], path[i:]
	}
	if !strings.HasSuffix(path, ".json") {
		path += ".json"
	}
	return path + query
}

// do is the central HTTP dispatcher. It enforces the read/write gate: non-GET
// requests are rejected with ErrReadOnly unless the client is in write mode.
func (c *Client) do(ctx context.Context, method, path string, body io.Reader) (*Response, error) {
	if method != http.MethodGet && c.Mode != mode.Write {
		return nil, fmt.Errorf("%w: run `lk mode write` to enable writes", ErrReadOnly)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.buildURL(path), body)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.setHeaders(req)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}
	return &Response{StatusCode: resp.StatusCode, Body: b, Header: resp.Header}, nil
}

// Get performs a GET request and returns the response.
func (c *Client) Get(ctx context.Context, path string) (*Response, error) {
	return c.do(ctx, http.MethodGet, path, nil)
}

// Post performs a POST request with an optional JSON-encoded payload.
func (c *Client) Post(ctx context.Context, path string, payload any) (*Response, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encoding request body: %w", err)
		}
		body = bytes.NewReader(b)
	}
	return c.do(ctx, http.MethodPost, path, body)
}

// Delete performs a DELETE request and returns the response.
func (c *Client) Delete(ctx context.Context, path string) (*Response, error) {
	return c.do(ctx, http.MethodDelete, path, nil)
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
}
