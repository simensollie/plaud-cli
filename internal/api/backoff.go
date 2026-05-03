package api

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// ErrRateLimited is returned by BackoffTransport when 429 retries are
// exhausted (5 retries per call). Callers surface this as a per-recording
// failure (F-08) rather than aborting the whole sync.
var ErrRateLimited = errors.New("rate limited (HTTP 429): retry budget exhausted")

// backoffSchedule is the default sleep schedule between 429 retries.
// Index i is the sleep before retry i+1; total budget is 5 retries.
var backoffSchedule = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	30 * time.Second,
}

// retryAfterCap caps the Retry-After header at 30s so a misbehaving
// upstream cannot stall a worker for arbitrary durations (Q9 rule 2).
const retryAfterCap = 30 * time.Second

// BackoffTransport is an http.RoundTripper that retries HTTP 429 with
// exponential backoff (F-06). 5xx and network errors surface immediately
// (inherits spec 0002 F-15 stance). Honors the Retry-After header capped
// at 30s; falls back to the schedule when absent or unparseable.
//
// Worker-local: each in-flight request retries independently. Cross-worker
// coordination is deferred to v0.4 (spec 0003 §7 Q4).
type BackoffTransport struct {
	// Inner is the wrapped transport. Defaults to http.DefaultTransport
	// when nil.
	Inner http.RoundTripper
	// Sleep is the sleep primitive. Tests inject a mock to advance time
	// instantly; production uses the default time.After.
	Sleep func(time.Duration) <-chan time.Time
}

// RoundTrip implements http.RoundTripper. Buffers the request body once so
// retries can replay it; for GET requests this is a no-op.
func (b *BackoffTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	inner := b.Inner
	if inner == nil {
		inner = http.DefaultTransport
	}
	sleepFn := b.Sleep
	if sleepFn == nil {
		sleepFn = func(d time.Duration) <-chan time.Time { return time.After(d) }
	}

	var bodyBytes []byte
	if req.Body != nil && req.Body != http.NoBody {
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("buffering request body for backoff retry: %w", err)
		}
		_ = req.Body.Close()
		bodyBytes = raw
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	// Initial attempt + len(backoffSchedule) retries.
	for i := 0; i <= len(backoffSchedule); i++ {
		if i > 0 && bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}
		resp, err := inner.RoundTrip(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}
		// 429: drain body and decide whether to retry.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		if i == len(backoffSchedule) {
			return nil, ErrRateLimited
		}

		wait := parseRetryAfter(resp)
		if wait <= 0 {
			wait = backoffSchedule[i]
		}
		if wait > retryAfterCap {
			wait = retryAfterCap
		}

		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-sleepFn(wait):
		}
	}

	return nil, ErrRateLimited
}

// parseRetryAfter extracts a duration from the Retry-After header. The
// header may be either a decimal integer (seconds) or an HTTP-date
// (RFC 1123 / 850 / asctime). Returns 0 when absent or unparseable.
func parseRetryAfter(resp *http.Response) time.Duration {
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		d := time.Until(t)
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}
