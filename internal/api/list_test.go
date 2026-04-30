package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestList_F06_PaginatesUntilExhausted asserts that List walks pages until it
// has retrieved data_file_total records, without an off-by-one and without
// re-paging once the total has been reached.
//
// Spec: specs/0001-auth-and-list/ F-06
func TestList_F06_PaginatesUntilExhausted(t *testing.T) {
	const total = 7
	pageSize := 3

	var (
		requestCount int
		gotSkips     []int
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/file/simple/web" {
			http.NotFound(w, r)
			return
		}
		requestCount++

		skip, _ := strconv.Atoi(r.URL.Query().Get("skip"))
		gotSkips = append(gotSkips, skip)

		// Build a page of up to pageSize ids: rec-<skip>, rec-<skip+1>, ...
		// start_time and duration are in milliseconds.
		var items []string
		for i := skip; i < skip+pageSize && i < total; i++ {
			items = append(items, fmt.Sprintf(`{
				"id":"rec-%d","filename":"f%d","start_time":1700000000000,
				"duration":60000,"is_trash":false,"is_trans":true,"is_summary":false
			}`, i, i))
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"status":0,"msg":"ok",
			"data_file_total":%d,
			"data_file_list":[%s]
		}`, total, strings.Join(items, ","))
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionUS, "tok", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := c.List(context.Background(), withListPageSize(pageSize))
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(got) != total {
		t.Fatalf("got %d recordings, want %d", len(got), total)
	}
	wantPages := 3 // ceil(7 / 3) == 3
	if requestCount != wantPages {
		t.Errorf("HTTP requests: got %d, want %d", requestCount, wantPages)
	}
	wantSkips := []int{0, 3, 6}
	for i, want := range wantSkips {
		if i >= len(gotSkips) || gotSkips[i] != want {
			t.Errorf("skip[%d] = %v, want %d (full: %v)", i, gotSkips, want, wantSkips)
			break
		}
	}
}

// TestList_F06_RecordingShape pins the field mapping from API JSON to the
// public Recording struct. Times decode from epoch seconds; duration from
// integer seconds.
//
// Spec: specs/0001-auth-and-list/ F-06, F-10
func TestList_F06_RecordingShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// start_time and duration are in milliseconds.
		// 1745000000000 ms = 2025-04-18T16:53:20Z; 2723000 ms = 45m23s.
		fmt.Fprint(w, `{
			"status":0,"msg":"ok",
			"data_file_total":1,
			"data_file_list":[{
				"id":"abc123",
				"filename":"Kickoff Meeting",
				"start_time":1745000000000,
				"duration":2723000,
				"is_trash":false,
				"is_trans":true,
				"is_summary":false
			}]
		}`)
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionUS, "tok", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	r := got[0]
	if r.ID != "abc123" {
		t.Errorf("ID: got %q, want %q", r.ID, "abc123")
	}
	if r.Filename != "Kickoff Meeting" {
		t.Errorf("Filename: got %q, want %q", r.Filename, "Kickoff Meeting")
	}
	wantStart := time.UnixMilli(1745000000000).UTC()
	if !r.StartTime.Equal(wantStart) {
		t.Errorf("StartTime: got %v, want %v", r.StartTime, wantStart)
	}
	wantDur := 2723 * time.Second
	if r.Duration != wantDur {
		t.Errorf("Duration: got %v, want %v", r.Duration, wantDur)
	}
	if !r.HasTranscript {
		t.Error("HasTranscript: got false, want true")
	}
	if r.HasSummary {
		t.Error("HasSummary: got true, want false")
	}
	if r.IsTrash {
		t.Error("IsTrash: got true, want false")
	}
}

// TestList_F08_SurfacesUnauthorized asserts that a 401 from the API is
// returned as a typed error so the CLI can produce the spec's actionable
// "Token expired or invalid" message and exit non-zero without a retry.
//
// Spec: specs/0001-auth-and-list/ F-08
func TestList_F08_SurfacesUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionUS, "tok", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = c.List(context.Background())
	if err == nil {
		t.Fatal("List against 401 returned nil error")
	}
	// We do not yet know whether ErrUnauthorized is the chosen sentinel or
	// just a wrapped ErrAPIError; either is acceptable as long as something
	// typed is reachable so the CLI can pattern-match.
	if !isAuthError(err) {
		t.Errorf("err = %v, want a 401-typed error", err)
	}
}
