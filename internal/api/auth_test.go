package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestDiscoverRegionAPI_F02_PostsCorrectBodyAndReturnsHost pins the
// region-discovery contract: POST /auth/otp-send-code with {username,
// user_area} returns the regional API host inside data.domains.api.
//
// Spec: specs/0001-auth-and-list/ F-02
func TestDiscoverRegionAPI_F02_PostsCorrectBodyAndReturnsHost(t *testing.T) {
	const (
		email     = "user@example.com"
		userArea  = "NO"
		regionURL = "https://api-euc1.plaud.ai"
	)

	var gotMethod, gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"status": 0,
			"msg": "ok",
			"data": {"domains": {"api": "`+regionURL+`"}}
		}`)
	}))
	t.Cleanup(srv.Close)

	got, err := DiscoverRegionAPI(context.Background(), srv.URL, email, userArea)
	if err != nil {
		t.Fatalf("DiscoverRegionAPI: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/auth/otp-send-code" {
		t.Errorf("path = %q, want /auth/otp-send-code", gotPath)
	}
	if gotBody["username"] != email {
		t.Errorf("body.username = %v, want %q", gotBody["username"], email)
	}
	if gotBody["user_area"] != userArea {
		t.Errorf("body.user_area = %v, want %q", gotBody["user_area"], userArea)
	}
	if got != regionURL {
		t.Errorf("returned host = %q, want %q", got, regionURL)
	}
}

// TestSendOTP_F02_PostsCorrectBodyAndReturnsExchangeToken pins that calling
// the regional otp-send-code returns the one-time exchange token used as
// input to VerifyOTP.
//
// Spec: specs/0001-auth-and-list/ F-02
func TestSendOTP_F02_PostsCorrectBodyAndReturnsExchangeToken(t *testing.T) {
	const (
		email         = "user@example.com"
		userArea      = "NO"
		exchangeToken = "exchange-abc123"
	)

	var gotMethod, gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"status": 0,
			"msg": "ok",
			"request_id": "req-1",
			"token": "`+exchangeToken+`"
		}`)
	}))
	t.Cleanup(srv.Close)

	got, err := SendOTP(context.Background(), srv.URL, email, userArea)
	if err != nil {
		t.Fatalf("SendOTP: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/auth/otp-send-code" {
		t.Errorf("path = %q, want /auth/otp-send-code", gotPath)
	}
	if gotBody["username"] != email {
		t.Errorf("body.username = %v, want %q", gotBody["username"], email)
	}
	if gotBody["user_area"] != userArea {
		t.Errorf("body.user_area = %v, want %q", gotBody["user_area"], userArea)
	}
	if got != exchangeToken {
		t.Errorf("returned token = %q, want %q", got, exchangeToken)
	}
}

// TestVerifyOTP_F02_ReturnsAccessToken pins the happy-path otp-login redemption.
//
// Spec: specs/0001-auth-and-list/ F-02
func TestVerifyOTP_F02_ReturnsAccessToken(t *testing.T) {
	const (
		exchangeToken = "exchange-abc123"
		code          = "123456"
		userArea      = "NO"
		accessToken   = "eyJ-real-jwt"
	)

	var gotMethod, gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"status": 0,
			"msg": "ok",
			"request_id": "req-2",
			"access_token": "`+accessToken+`",
			"token_type": "bearer",
			"has_password": true,
			"is_new_user": false
		}`)
	}))
	t.Cleanup(srv.Close)

	got, err := VerifyOTP(context.Background(), srv.URL, exchangeToken, code, userArea)
	if err != nil {
		t.Fatalf("VerifyOTP: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/auth/otp-login" {
		t.Errorf("path = %q, want /auth/otp-login", gotPath)
	}
	for _, want := range []string{"token", "code", "user_area", "require_set_password", "team_enabled"} {
		if _, ok := gotBody[want]; !ok {
			t.Errorf("body missing field %q (got: %v)", want, gotBody)
		}
	}
	if gotBody["token"] != exchangeToken {
		t.Errorf("body.token = %v, want %q", gotBody["token"], exchangeToken)
	}
	if gotBody["code"] != code {
		t.Errorf("body.code = %v, want %q", gotBody["code"], code)
	}
	if got != accessToken {
		t.Errorf("returned access_token = %q, want %q", got, accessToken)
	}
}

// TestVerifyOTP_F02_PasswordNotSetReturnsTypedError asserts that an account
// without a password is surfaced as ErrPasswordNotSet, so the CLI can show
// the "set a password via web first" message rather than persisting an
// access_token that may not yet work for protected calls.
//
// Spec: specs/0001-auth-and-list/ F-02
func TestVerifyOTP_F02_PasswordNotSetReturnsTypedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"status": 0,
			"msg": "ok",
			"access_token": "should-not-be-used",
			"token_type": "bearer",
			"has_password": false,
			"is_new_user": true,
			"set_password_token": "set-pw-tok"
		}`)
	}))
	t.Cleanup(srv.Close)

	_, err := VerifyOTP(context.Background(), srv.URL, "tok", "123456", "NO")
	if !errors.Is(err, ErrPasswordNotSet) {
		t.Fatalf("err = %v, want errors.Is ErrPasswordNotSet", err)
	}
}

// TestVerifyOTP_F02_BadCodeReturnsTypedError pins the failure-mode envelope.
// Plaud's API uses HTTP 200 with a non-zero body `status` field for logical
// errors; we surface this as ErrInvalidOTP (or wrapped ErrAPIError for
// unrecognized codes).
//
// Spec: specs/0001-auth-and-list/ F-02
func TestVerifyOTP_F02_BadCodeReturnsTypedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"status": 40010,
			"msg": "invalid otp code",
			"request_id": "req-3"
		}`)
	}))
	t.Cleanup(srv.Close)

	_, err := VerifyOTP(context.Background(), srv.URL, "tok", "000000", "NO")
	if err == nil {
		t.Fatal("VerifyOTP returned nil error for bad code, want error")
	}
	// Either a specific ErrInvalidOTP, or a wrapped ErrAPIError that exposes
	// the upstream status. Both are acceptable; the goal is "typed enough that
	// the CLI can pattern-match on it instead of string-comparing msg".
	if !errors.Is(err, ErrInvalidOTP) && !errors.Is(err, ErrAPIError) {
		t.Fatalf("err = %v, want errors.Is ErrInvalidOTP or ErrAPIError", err)
	}
}
