package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestFetchTranscript_F01_ReturnsBodyBytes verifies that the helper returns
// the raw body bytes from a content-storage signed URL.
func TestFetchTranscript_F01_ReturnsBodyBytes(t *testing.T) {
	const want = `[{"start_time":0,"end_time":1000,"content":"hi","speaker":"A","original_speaker":"A"}]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(want))
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionEU, "tok", WithBaseURL("http://unused"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := c.fetchSignedJSON(context.Background(), srv.URL+"/transcript.json.gz")
	if err != nil {
		t.Fatalf("fetchSignedJSON: %v", err)
	}
	if string(got) != want {
		t.Errorf("body: got %q, want %q", string(got), want)
	}
}

// TestFetchTranscript_F13_DoesNotSendAuthorizationToS3 fails the test if any
// Authorization header is sent on the S3 leg. F-13 demands tokens never leak
// to third parties.
func TestFetchTranscript_F13_DoesNotSendAuthorizationToS3(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.Header.Get("Authorization"); v != "" {
			t.Errorf("Authorization header leaked to S3: %q", v)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionEU, "secret-bearer-token", WithBaseURL("http://unused"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := c.fetchSignedJSON(context.Background(), srv.URL+"/transcript.json.gz"); err != nil {
		t.Fatalf("fetchSignedJSON: %v", err)
	}
}

// TestFetchTranscript_F01_404OnContentStorageReturnsError verifies that a
// non-2xx from S3 surfaces as a wrapped error (caller decides what to do).
func TestFetchTranscript_F01_404OnContentStorageReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionEU, "tok", WithBaseURL("http://unused"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = c.fetchSignedJSON(context.Background(), srv.URL+"/transcript.json.gz")
	if err == nil {
		t.Fatal("fetchSignedJSON against 404 returned nil error")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v, want it to mention status 404", err)
	}
}
