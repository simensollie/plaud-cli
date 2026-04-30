package api

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestClient_F01_SetsAuthHeader pins the contract that every HTTP request
// issued through the client carries Authorization: bearer <token>. The header
// is the only thing standing between the client and a 401, so a regression
// here would silently break every higher-level call.
//
// Spec: specs/0001-auth-and-list/ F-01
func TestClient_F01_SetsAuthHeader(t *testing.T) {
	const token = "eyJ-test-token"

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionUS, token, WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/whatever", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := c.do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	_, _ = io.Copy(io.Discard, resp.Body)

	const want = "bearer " + token
	if gotAuth != want {
		t.Errorf("Authorization header = %q, want %q", gotAuth, want)
	}
}

// TestClient_RejectsEmptyRegion guards against constructing a client with no
// region, which would silently fall through to an empty base URL.
//
// Spec: specs/0001-auth-and-list/ F-01 (input validation)
func TestClient_RejectsEmptyRegion(t *testing.T) {
	_, err := New(Region(""), "tok")
	if err == nil {
		t.Fatal("New with empty region returned nil error, want error")
	}
	if !errors.Is(err, ErrEmptyRegion) {
		t.Errorf("err = %v, want errors.Is ErrEmptyRegion", err)
	}
}

// TestClient_RejectsEmptyToken guards against constructing a client without
// a token. A missing token plus a permissive endpoint would otherwise silently
// produce 401-on-everything with no setup-time signal.
//
// Spec: specs/0001-auth-and-list/ F-01 (input validation)
func TestClient_RejectsEmptyToken(t *testing.T) {
	_, err := New(RegionUS, "")
	if err == nil {
		t.Fatal("New with empty token returned nil error, want error")
	}
	if !errors.Is(err, ErrEmptyToken) {
		t.Errorf("err = %v, want errors.Is ErrEmptyToken", err)
	}
}
