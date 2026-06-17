package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultTimeout = 30 * time.Second

// Client is an HTTP client for the Linkana backend.
type Client struct {
	BaseURL    string
	Token      string
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

// Get performs a GET request and returns the response.
func (c *Client) Get(ctx context.Context, path string) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.buildURL(path), nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	return &Response{StatusCode: resp.StatusCode, Body: body, Header: resp.Header}, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
}
