package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const fakeTempURL = "https://euc1-prod-plaud-bucket.s3.amazonaws.com/audiofiles/abc.mp3?X-Amz-Signature=test"

func TestTempURL_F15_ReturnsSignedURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/file/temp-url/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":0,"msg":"ok","temp_url":%q,"temp_url_opus":null}`, fakeTempURL)
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionEU, "tok", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := c.TempURL(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("TempURL: %v", err)
	}
	if got != fakeTempURL {
		t.Errorf("got %q, want %q", got, fakeTempURL)
	}
}

func TestTempURL_F15_OpusFieldIgnored(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":0,"msg":"ok","temp_url":%q,"temp_url_opus":"https://example.com/opus-link-not-returned"}`, fakeTempURL)
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionEU, "tok", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := c.TempURL(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("TempURL: %v", err)
	}
	if got != fakeTempURL {
		t.Errorf("got %q, want %q (opus URL must not leak through)", got, fakeTempURL)
	}
}

func TestTempURL_F10_Surfaces401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionEU, "tok", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = c.TempURL(context.Background(), "abc123")
	if err == nil {
		t.Fatal("TempURL against 401 returned nil error")
	}
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("err = %v, want errors.Is ErrUnauthorized", err)
	}
}

func TestTempURL_F10_NonZeroStatusReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":5,"msg":"some plaud-side failure","temp_url":"","temp_url_opus":null}`)
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionEU, "tok", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = c.TempURL(context.Background(), "abc123")
	if err == nil {
		t.Fatal("TempURL against status=5 returned nil error")
	}
	if !errors.Is(err, ErrAPIError) {
		t.Errorf("err = %v, want errors.Is ErrAPIError", err)
	}
}

// TestTempURL_F10_EnvelopeInvalidAuthHeaderReturnsUnauthorized covers the
// real-API auth-failure path: Plaud returns HTTP 200 with envelope
// {status: -3900, msg: "invalid auth header"} instead of HTTP 401. F-10
// requires the same ErrUnauthorized sentinel either way.
//
// Spec: specs/0002-download-recordings/ F-10; notes.md 2026-05-03
func TestTempURL_F10_EnvelopeInvalidAuthHeaderReturnsUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":-3900,"msg":"invalid auth header","temp_url":"","temp_url_opus":null}`)
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionEU, "tok", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = c.TempURL(context.Background(), "abc123")
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("err = %v, want errors.Is ErrUnauthorized", err)
	}
}
