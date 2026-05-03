package api

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
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

// AudioHead is the result of a HEAD against the signed audio URL.
type AudioHead struct {
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

// HeadAudio HEADs the signed S3 URL and extracts the ETag and Content-Length.
// Never sends Authorization (S3 auth is in the URL).
func (c *Client) HeadAudio(ctx context.Context, signedURL string) (*AudioHead, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, signedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building HEAD audio request: %w", err)
	}

	resp, err := c.audioClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HEAD audio: %w", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, ErrSignedURLExpired
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HEAD audio: http %d", resp.StatusCode)
	}

	return &AudioHead{
		ETag:      unquoteETag(resp.Header.Get("ETag")),
		SizeBytes: resp.ContentLength,
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
