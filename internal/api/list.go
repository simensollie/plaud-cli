package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultListPageSize = 200

// ErrUnauthorized indicates the bearer token was rejected (HTTP 401). The
// CLI surfaces this as the spec's "Token expired or invalid. Run `plaud
// login` again." message and exits non-zero without retry.
var ErrUnauthorized = errors.New("unauthorized: bearer token rejected")

// Recording is one item from /file/simple/web, normalized for callers.
// The wire format uses epoch seconds; we hand callers time.Time / Duration
// so date and duration formatting can stay out of every consumer.
type Recording struct {
	ID            string
	Filename      string
	StartTime     time.Time
	Duration      time.Duration
	IsTrash       bool
	HasTranscript bool
	HasSummary    bool
	// FileMD5 is the MD5 of Plaud's original .opus upload. Recorded for audit
	// (metadata.audio.original_upload_md5); never used as an idempotency key
	// because the served audio bytes are .mp3, not .opus. F-07(a).
	FileMD5 string
}

// rawRecording mirrors the JSON wire format. Kept narrow to the fields the
// CLI uses; the API exposes more (scene, version_ms, etc.) but we add them
// only when a feature needs them.
type rawRecording struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	StartTime int64  `json:"start_time"`
	Duration  int64  `json:"duration"`
	IsTrash   bool   `json:"is_trash"`
	IsTrans   bool   `json:"is_trans"`
	IsSummary bool   `json:"is_summary"`
	FileMD5   string `json:"file_md5"`
}

func (r rawRecording) toRecording() Recording {
	// start_time and duration are in milliseconds despite the lack of an
	// _ms suffix on the field name. Confirmed on first real-API call against
	// api-euc1.plaud.ai on 2026-05-01: treating them as seconds produced
	// year-58000 timestamps and absurd durations.
	return Recording{
		ID:            r.ID,
		Filename:      r.Filename,
		StartTime:     time.UnixMilli(r.StartTime).UTC(),
		Duration:      time.Duration(r.Duration) * time.Millisecond,
		IsTrash:       r.IsTrash,
		HasTranscript: r.IsTrans,
		HasSummary:    r.IsSummary,
		FileMD5:       r.FileMD5,
	}
}

type listResponse struct {
	Status int            `json:"status"`
	Msg    string         `json:"msg"`
	Total  int            `json:"data_file_total"`
	Files  []rawRecording `json:"data_file_list"`
}

type listConfig struct {
	pageSize int
}

type listOption func(*listConfig)

// withListPageSize overrides the default page size. Package-private; tests
// pin this to a small value to exercise the multi-page loop without large
// fixtures.
func withListPageSize(n int) listOption {
	return func(c *listConfig) { c.pageSize = n }
}

// List enumerates every active recording on the account, walking pages
// until the server's data_file_total has been reached. Returns
// ErrUnauthorized on 401 so the CLI can surface a "log in again" message
// without retrying.
func (c *Client) List(ctx context.Context, opts ...listOption) ([]Recording, error) {
	cfg := &listConfig{pageSize: defaultListPageSize}
	for _, opt := range opts {
		opt(cfg)
	}

	var out []Recording
	skip := 0
	for {
		url := fmt.Sprintf(
			"%s/file/simple/web?skip=%d&limit=%d&is_trash=0&sort_by=start_time&is_desc=true",
			c.baseURL, skip, cfg.pageSize,
		)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("building list request: %w", err)
		}

		resp, err := c.do(req)
		if err != nil {
			return nil, fmt.Errorf("calling /file/simple/web: %w", err)
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("reading /file/simple/web body: %w", readErr)
		}

		if resp.StatusCode == http.StatusUnauthorized {
			return nil, ErrUnauthorized
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("%w: http %d", ErrAPIError, resp.StatusCode)
		}

		var page listResponse
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("decoding /file/simple/web: %w", err)
		}
		if page.Status != 0 {
			return nil, fmt.Errorf("%w: status=%d msg=%q", ErrAPIError, page.Status, page.Msg)
		}

		for _, raw := range page.Files {
			out = append(out, raw.toRecording())
		}
		skip += len(page.Files)
		if skip >= page.Total || len(page.Files) == 0 {
			break
		}
	}
	return out, nil
}

// isAuthError reports whether err carries the ErrUnauthorized sentinel.
// Used by tests so the assertion does not lock onto a concrete type.
func isAuthError(err error) bool {
	return errors.Is(err, ErrUnauthorized)
}
