package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/simensollie/plaud-cli/internal/api"
	"github.com/simensollie/plaud-cli/internal/auth"
)

// runListCmd builds a list command with the resolver pinned to srv.URL,
// runs it, and returns combined output + any error.
func runListCmd(t *testing.T, srvURL string) (string, error) {
	t.Helper()
	cmd := newListCmd(withListBaseURLResolver(func(_ api.Region) (string, error) {
		return srvURL, nil
	}))
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())
	err := cmd.Execute()
	return buf.String(), err
}

// TestList_F06_TableOutput asserts the list command renders saved
// recordings as a sorted, ISO-8601-dated, HH:MM:SS-duration table.
//
// Spec: specs/0001-auth-and-list/ F-06, F-10
func TestList_F06_TableOutput(t *testing.T) {
	setTempConfig(t)

	if err := auth.Save(auth.Credentials{
		Token: "tok", Region: "eu", Email: "u@example.com", ObtainedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}

	// Two recordings, deterministic times: 2026-04-30 14:30 UTC (45m23s)
	// and 2026-04-29 09:00 UTC (30m exactly).
	// API returns start_time and duration in milliseconds.
	fixed1 := time.Date(2026, 4, 30, 14, 30, 0, 0, time.UTC).UnixMilli()
	fixed2 := time.Date(2026, 4, 29, 9, 0, 0, 0, time.UTC).UnixMilli()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"status":0,"msg":"ok",
			"data_file_total":2,
			"data_file_list":[
				{"id":"abc12345","filename":"Kickoff Meeting","start_time":%d,"duration":2723000,"is_trash":false,"is_trans":true,"is_summary":false},
				{"id":"def67890","filename":"1:1 with Qamar","start_time":%d,"duration":1800000,"is_trash":false,"is_trans":true,"is_summary":true}
			]
		}`, fixed1, fixed2)
	}))
	t.Cleanup(srv.Close)

	out, err := runListCmd(t, srv.URL)
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}

	for _, want := range []string{
		"DATE", "TITLE", "DURATION", "ID",
		"2026-04-30 14:30",
		"Kickoff Meeting",
		"00:45:23",
		"abc12345",
		"2026-04-29 09:00",
		"1:1 with Qamar",
		"00:30:00",
		"def67890",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, out)
		}
	}

	// Newest first: assert the recent date appears before the older one.
	idxRecent := strings.Index(out, "2026-04-30")
	idxOlder := strings.Index(out, "2026-04-29")
	if idxRecent < 0 || idxOlder < 0 || idxRecent > idxOlder {
		t.Errorf("recordings not sorted newest-first; got order indices %d, %d\n%s", idxRecent, idxOlder, out)
	}
}

// TestList_F07_NotLoggedIn asserts that running `plaud list` without
// credentials prints the spec's exact "Not logged in" message and exits
// non-zero.
//
// Spec: specs/0001-auth-and-list/ F-07
func TestList_F07_NotLoggedIn(t *testing.T) {
	setTempConfig(t)

	out, err := runListCmd(t, "http://unused.invalid")
	if err == nil {
		t.Fatal("list with no credentials returned nil error")
	}
	combined := out + err.Error()
	for _, want := range []string{"Not logged in", "plaud login"} {
		if !strings.Contains(combined, want) {
			t.Errorf("expected message fragment %q in output/err, got:\n%s", want, combined)
		}
	}
}

// TestList_F08_TokenInvalid asserts that a 401 from the server produces
// the spec's exact "Token expired or invalid" message and exits non-zero
// without retrying.
//
// Spec: specs/0001-auth-and-list/ F-08
func TestList_F08_TokenInvalid(t *testing.T) {
	setTempConfig(t)

	if err := auth.Save(auth.Credentials{
		Token: "tok", Region: "eu", Email: "u@example.com", ObtainedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}

	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"status":401,"msg":"unauthorized"}`)
	}))
	t.Cleanup(srv.Close)

	out, err := runListCmd(t, srv.URL)
	if err == nil {
		t.Fatal("list against 401 returned nil error")
	}
	combined := out + err.Error()
	for _, want := range []string{"Token expired or invalid", "plaud login"} {
		if !strings.Contains(combined, want) {
			t.Errorf("expected message fragment %q in output/err, got:\n%s", want, combined)
		}
	}
	if requestCount > 1 {
		t.Errorf("expected at most 1 request to server (no retry); got %d", requestCount)
	}
}
