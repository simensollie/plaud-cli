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
	baseURL     string
	token       string
	httpClient  *http.Client
	audioClient *http.Client
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

// WithAudioHTTPClient swaps the audio-leg http.Client. Intended for tests
// that need transport-level instrumentation on the unbounded-timeout client
// used for streaming audio bytes from S3.
func WithAudioHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.audioClient = hc }
}

// WithBackoffTransport wraps both the API and audio HTTP clients in
// BackoffTransport so 429 retries with exponential backoff (spec 0003 F-06)
// apply to every request. 5xx and network errors still surface immediately.
// Spec 0002 F-15 acquires this behavior automatically when the CLI uses it.
func WithBackoffTransport() Option {
	return func(c *Client) {
		if c.httpClient != nil {
			inner := c.httpClient.Transport
			if inner == nil {
				inner = http.DefaultTransport
			}
			c.httpClient.Transport = &BackoffTransport{Inner: inner}
		}
		if c.audioClient != nil {
			inner := c.audioClient.Transport
			if inner == nil {
				inner = http.DefaultTransport
			}
			c.audioClient.Transport = &BackoffTransport{Inner: inner}
		}
	}
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
		baseURL:     base,
		token:       token,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		audioClient: &http.Client{Timeout: 0},
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
