package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// fetchSignedJSON GETs a content-storage signed URL and returns the response
// body bytes. No Authorization header is sent: the URL itself carries SigV4
// credentials, and forwarding our bearer would leak it to a third party (F-13).
// Go's http.Transport handles Content-Encoding: gzip transparently, so the
// returned bytes are the decoded payload.
func (c *Client) fetchSignedJSON(ctx context.Context, signedURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, signedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building signed-URL request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling content-storage URL: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading content-storage body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("content-storage returned http %d", resp.StatusCode)
	}
	return body, nil
}
