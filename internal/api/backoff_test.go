package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// recordingTransport counts calls and returns the responses queued by the
// test in order.
type recordingTransport struct {
	codes []int
	count atomic.Int64
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	idx := int(r.count.Add(1)) - 1
	if idx >= len(r.codes) {
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: http.Header{}, Request: req}, nil
	}
	code := r.codes[idx]
	resp := &http.Response{StatusCode: code, Body: http.NoBody, Header: http.Header{}, Request: req}
	return resp, nil
}

func newRequest(t *testing.T, ctx context.Context) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.invalid/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return req
}

func TestBackoff_F06_RetriesOn429ThenSucceeds(t *testing.T) {
	inner := &recordingTransport{codes: []int{429, 429, 200}}
	var slept []time.Duration
	bt := &BackoffTransport{
		Inner: inner,
		Sleep: func(d time.Duration) <-chan time.Time {
			slept = append(slept, d)
			ch := make(chan time.Time, 1)
			ch <- time.Now()
			return ch
		},
	}

	resp, err := bt.RoundTrip(newRequest(t, context.Background()))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status=%d, want 200", resp.StatusCode)
	}
	if got := inner.count.Load(); got != 3 {
		t.Errorf("inner called %d times, want 3", got)
	}
	if len(slept) != 2 || slept[0] != 1*time.Second || slept[1] != 2*time.Second {
		t.Errorf("sleeps=%v, want [1s 2s]", slept)
	}
}

func TestBackoff_F06_BudgetExhaustedAfter5Retries(t *testing.T) {
	inner := &recordingTransport{codes: []int{429, 429, 429, 429, 429, 429}}
	bt := &BackoffTransport{
		Inner: inner,
		Sleep: func(_ time.Duration) <-chan time.Time {
			ch := make(chan time.Time, 1)
			ch <- time.Now()
			return ch
		},
	}

	resp, err := bt.RoundTrip(newRequest(t, context.Background()))
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err=%v, want ErrRateLimited", err)
	}
	if resp != nil {
		t.Errorf("expected nil response on rate-limit exhaustion, got %v", resp)
	}
	// 1 initial + 5 retries = 6 calls total before giving up.
	if got := inner.count.Load(); got != 6 {
		t.Errorf("inner called %d times, want 6", got)
	}
}

func TestBackoff_F06_HonorsRetryAfterCappedAt30s(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cap" {
			w.Header().Set("Retry-After", "120") // 120s should cap to 30s
		} else {
			w.Header().Set("Retry-After", "5")
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	var slept []time.Duration
	bt := &BackoffTransport{
		Inner: http.DefaultTransport,
		Sleep: func(d time.Duration) <-chan time.Time {
			slept = append(slept, d)
			ch := make(chan time.Time, 1)
			ch <- time.Now()
			return ch
		},
	}

	// Hit /cap once: gives Retry-After=120 (must clamp to 30s).
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/cap", nil)
	_, err := bt.RoundTrip(req)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
	if len(slept) == 0 {
		t.Fatalf("expected some sleeps, got none")
	}
	for i, d := range slept {
		if d > 30*time.Second {
			t.Errorf("sleep[%d]=%v exceeds 30s cap", i, d)
		}
	}
	for _, d := range slept {
		if d != 30*time.Second {
			t.Errorf("expected 30s cap on Retry-After=120, got %v", d)
		}
	}
}

func TestBackoff_F06_5xxNotRetried(t *testing.T) {
	inner := &recordingTransport{codes: []int{500, 200}}
	bt := &BackoffTransport{
		Inner: inner,
		Sleep: func(_ time.Duration) <-chan time.Time {
			ch := make(chan time.Time, 1)
			ch <- time.Now()
			return ch
		},
	}

	resp, err := bt.RoundTrip(newRequest(t, context.Background()))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if resp.StatusCode != 500 {
		t.Errorf("status=%d, want 500 (no retry)", resp.StatusCode)
	}
	if got := inner.count.Load(); got != 1 {
		t.Errorf("inner called %d times, want exactly 1 (no retry)", got)
	}
}

func TestBackoff_F06_NetworkErrorSurfacesImmediately(t *testing.T) {
	wantErr := errors.New("connection reset")
	bt := &BackoffTransport{
		Inner: roundTripFunc(func(_ *http.Request) (*http.Response, error) { return nil, wantErr }),
		Sleep: func(_ time.Duration) <-chan time.Time {
			ch := make(chan time.Time, 1)
			ch <- time.Now()
			return ch
		},
	}
	_, err := bt.RoundTrip(newRequest(t, context.Background()))
	if !errors.Is(err, wantErr) {
		t.Fatalf("err=%v, want %v", err, wantErr)
	}
}

func TestBackoff_F06_ContextCancelDuringSleep(t *testing.T) {
	inner := &recordingTransport{codes: []int{429, 200}}
	never := make(chan time.Time) // never fires; only ctx.Done can win
	bt := &BackoffTransport{
		Inner: inner,
		Sleep: func(_ time.Duration) <-chan time.Time { return never },
	}
	ctx, cancel := context.WithCancel(context.Background())

	type result struct {
		resp *http.Response
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := bt.RoundTrip(newRequest(t, ctx))
		done <- result{resp, err}
	}()

	cancel()

	select {
	case r := <-done:
		if !errors.Is(r.err, context.Canceled) {
			t.Errorf("err=%v, want context.Canceled", r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RoundTrip did not return after context cancel")
	}
}

// Sanity: a transport configured with no Inner falls back to
// http.DefaultTransport.
func TestBackoff_F06_DefaultsToHTTPDefaultTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	bt := &BackoffTransport{}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := bt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status=%d", resp.StatusCode)
	}
}

// roundTripFunc is a small helper that adapts a closure into RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// Sanity: ErrRateLimited error message names the cause, not implementation
// detail.
func TestBackoff_F06_ErrRateLimitedIsActionable(t *testing.T) {
	if !strings.Contains(strings.ToLower(ErrRateLimited.Error()), "rate") {
		t.Errorf("ErrRateLimited message should mention rate-limiting: %q", ErrRateLimited)
	}
}
