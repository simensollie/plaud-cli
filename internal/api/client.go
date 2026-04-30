package api

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Client is the low-level HTTP client for the Plaud API. Higher-level
// operations (auth, list, download) live in sibling files in this package and
// share the client through the unexported do method.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// Option mutates a Client during construction.
type Option func(*Client)

// WithBaseURL overrides the region-derived base URL. Intended for tests
// (httptest.NewServer.URL) and for future per-environment overrides; not part
// of the public CLI surface.
func WithBaseURL(url string) Option {
	return func(c *Client) { c.baseURL = url }
}

// WithHTTPClient swaps the underlying http.Client. Intended for tests that
// need transport-level instrumentation.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// ErrEmptyRegion is returned by New when the region argument is the zero value.
var ErrEmptyRegion = errors.New("empty region")

// ErrEmptyToken is returned by New when the token argument is empty.
var ErrEmptyToken = errors.New("empty token")

// New constructs a Client for the given region and bearer token. Both
// arguments are required; constructing with an empty value fails fast rather
// than producing a client that would 401 on every call.
func New(region Region, token string, opts ...Option) (*Client, error) {
	if region == "" {
		return nil, ErrEmptyRegion
	}
	if token == "" {
		return nil, ErrEmptyToken
	}

	base, err := BaseURL(region)
	if err != nil {
		return nil, fmt.Errorf("resolving region: %w", err)
	}

	c := &Client{
		baseURL:    base,
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// BaseURL returns the resolved base URL the client is configured with. Useful
// for diagnostics and for callers that need to compose absolute URLs.
func (c *Client) BaseURL() string { return c.baseURL }

// do executes the request after injecting the Authorization header. Lowercase
// "bearer" matches what the consumer Plaud web app sends; verified against
// reverse-engineered prior art and to be confirmed by Phase 2 capture.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "bearer "+c.token)
	return c.httpClient.Do(req)
}
