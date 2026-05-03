package api

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrSignedURLExpired indicates the signed S3 URL was rejected by S3 with
// a 401 or 403. F-15 mandates the caller re-fetch the temp-url endpoint
// once and retry with a fresh URL.
var ErrSignedURLExpired = errors.New("signed S3 URL expired or invalid (HTTP 401/403)")

// defaultAudioIdleTimeout is the production idle window for streamed audio
// reads (F-15). Tests override via downloadOption.
const defaultAudioIdleTimeout = 30 * time.Second

// AudioProbe is the result of a metadata-only probe against the signed
// audio URL: enough to compare ETag for F-07(a) idempotency without
// streaming the bytes.
type AudioProbe struct {
	// ETag is canonical for F-07(a) idempotency. The S3-quoted form is
	// unquoted before the value lands here.
	ETag      string
	SizeBytes int64
}

type downloadConfig struct {
	idleTimeout time.Duration
}

// downloadOption mutates a downloadConfig. Package-private; tests use it to
// shrink the idle window for F-15 stall tests.
type downloadOption func(*downloadConfig)

func withIdleTimeout(d time.Duration) downloadOption {
	return func(c *downloadConfig) { c.idleTimeout = d }
}

// ProbeAudio fetches just enough of the signed S3 object to read its ETag
// and total size, without streaming the full body. Plaud's temp_url is a
// SigV4 presigned URL signed for GET only (the HTTP method is part of the
// signature canonical), so a real HTTP HEAD against it returns 403. We
// issue a one-byte ranged GET instead: S3 honours it with 206 Partial
// Content and exposes the total length in Content-Range. Never sends
// Authorization (S3 auth is in the URL).
func (c *Client) ProbeAudio(ctx context.Context, signedURL string) (*AudioProbe, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, signedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building probe audio request: %w", err)
	}
	req.Header.Set("Range", "bytes=0-0")

	resp, err := c.audioClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("probe audio: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, ErrSignedURLExpired
	}
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("probe audio: http %d", resp.StatusCode)
	}

	size := resp.ContentLength
	if resp.StatusCode == http.StatusPartialContent {
		if total, ok := parseContentRangeTotal(resp.Header.Get("Content-Range")); ok {
			size = total
		}
	}

	return &AudioProbe{
		ETag:      unquoteETag(resp.Header.Get("ETag")),
		SizeBytes: size,
	}, nil
}

// DownloadAudio streams bytes from the signed S3 URL into dst, computing the
// MD5 of the streamed bytes inline. Never sends Authorization (S3 auth is in
// the URL). Uses the audio HTTP client (no total timeout); the response body
// is wrapped in an idleTimeoutReader.
//
// On HTTP 401 or 403 from S3, returns ErrSignedURLExpired so the caller can
// re-call TempURL and retry once (F-15). Other 4xx and all 5xx surface as
// wrapped errors with the underlying status.
func (c *Client) DownloadAudio(ctx context.Context, signedURL string, dst io.Writer, opts ...downloadOption) (n int64, etag string, localMD5 string, err error) {
	cfg := &downloadConfig{idleTimeout: defaultAudioIdleTimeout}
	for _, opt := range opts {
		opt(cfg)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, signedURL, nil)
	if err != nil {
		return 0, "", "", fmt.Errorf("building GET audio request: %w", err)
	}

	resp, err := c.audioClient.Do(req)
	if err != nil {
		return 0, "", "", fmt.Errorf("GET audio: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return 0, "", "", ErrSignedURLExpired
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return 0, "", "", fmt.Errorf("GET audio: http %d", resp.StatusCode)
	}

	idle := newIdleTimeoutReader(resp.Body, cfg.idleTimeout)
	defer func() { _ = idle.Close() }()

	h := md5.New()
	written, copyErr := io.Copy(io.MultiWriter(dst, h), idle)
	if copyErr != nil {
		return written, "", "", fmt.Errorf("streaming audio: %w", copyErr)
	}

	return written, unquoteETag(resp.Header.Get("ETag")), hex.EncodeToString(h.Sum(nil)), nil
}

// unquoteETag strips S3's surrounding double quotes from an ETag header.
// S3 always quotes ETag values; the comparison-friendly form is unquoted.
func unquoteETag(v string) string {
	return strings.Trim(v, `"`)
}

// parseContentRangeTotal extracts the total-size component from a
// Content-Range header of the form "bytes 0-0/<total>". Returns false if
// the header is missing or malformed.
func parseContentRangeTotal(v string) (int64, bool) {
	i := strings.LastIndex(v, "/")
	if i < 0 || i == len(v)-1 {
		return 0, false
	}
	tail := v[i+1:]
	if tail == "*" {
		return 0, false
	}
	n, err := strconv.ParseInt(tail, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
