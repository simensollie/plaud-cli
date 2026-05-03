package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type tempURLResponse struct {
	Status      int    `json:"status"`
	Msg         string `json:"msg"`
	TempURL     string `json:"temp_url"`
	TempURLOpus string `json:"temp_url_opus"`
}

// TempURL fetches the signed audio URL for a recording. Single-call wrapper
// around GET /file/temp-url/{id}. The opus URL is currently ignored (v0.2
// scope; see spec 0002 F-15 and notes.md 2026-05-02).
func (c *Client) TempURL(ctx context.Context, id string) (string, error) {
	url := fmt.Sprintf("%s/file/temp-url/%s", c.baseURL, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("building temp-url request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return "", fmt.Errorf("calling /file/temp-url: %w", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return "", fmt.Errorf("reading /file/temp-url body: %w", readErr)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return "", ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%w: http %d", ErrAPIError, resp.StatusCode)
	}

	var payload tempURLResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decoding /file/temp-url: %w", err)
	}
	if payload.Status != 0 {
		return "", fmt.Errorf("%w: status=%d msg=%q", ErrAPIError, payload.Status, payload.Msg)
	}
	return payload.TempURL, nil
}
