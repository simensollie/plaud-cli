package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// GlobalAPIBase is the region-discovery host. Hitting it with an email
// returns the regional host that account belongs to.
const GlobalAPIBase = "https://api.plaud.ai"

var (
	// ErrInvalidOTP is returned when the server signals the OTP code was
	// wrong. We do not yet know Plaud's exact status code for this; until
	// confirmed, callers should also accept ErrAPIError for failed otp-login
	// calls and disambiguate by msg.
	ErrInvalidOTP = errors.New("invalid OTP code")

	// ErrPasswordNotSet is returned when otp-login succeeds at the envelope
	// level but the response includes set_password_token, meaning the
	// account has not yet established a password and the access_token may
	// not be usable for protected calls. The CLI surfaces this as a clear
	// "set a password via web first" message.
	ErrPasswordNotSet = errors.New("account has no password set")

	// ErrAPIError wraps any non-zero envelope status that we do not have a
	// more specific sentinel for. Inspect via errors.As to read the
	// upstream status + msg.
	ErrAPIError = errors.New("Plaud API error")
)

// AuthOption customizes the unauthenticated HTTP client used during login.
type AuthOption func(*authConfig)

type authConfig struct {
	httpClient *http.Client
}

// WithAuthHTTPClient overrides the http.Client used during pre-auth calls.
// Intended for tests.
func WithAuthHTTPClient(hc *http.Client) AuthOption {
	return func(c *authConfig) { c.httpClient = hc }
}

func newAuthConfig(opts ...AuthOption) *authConfig {
	c := &authConfig{httpClient: &http.Client{Timeout: 30 * time.Second}}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// envelope mirrors Plaud's common JSON response wrapper. Endpoints set
// different subsets of fields; omitempty on the request side and pointer /
// json.RawMessage on the response side keep this single struct usable across
// otp-send-code, otp-login, etc.
type envelope struct {
	Status    int    `json:"status"`
	Msg       string `json:"msg"`
	RequestID string `json:"request_id,omitempty"`

	// otp-send-code (regional)
	Token string `json:"token,omitempty"`

	// otp-login
	AccessToken      string `json:"access_token,omitempty"`
	TokenType        string `json:"token_type,omitempty"`
	HasPassword      *bool  `json:"has_password,omitempty"`
	IsNewUser        *bool  `json:"is_new_user,omitempty"`
	SetPasswordToken string `json:"set_password_token,omitempty"`

	// region discovery
	Data json.RawMessage `json:"data,omitempty"`
}

func (e *envelope) toError() error {
	if e.Status == 0 {
		return nil
	}
	return fmt.Errorf("%w: status=%d msg=%q", ErrAPIError, e.Status, e.Msg)
}

// postJSON posts the body as JSON to baseURL+path and decodes the response
// envelope. Returns the envelope plus a non-nil error if the transport
// failed or the HTTP status is non-2xx. Logical errors carried in the
// envelope (status != 0) are surfaced by the caller via env.toError().
func postJSON(ctx context.Context, hc *http.Client, baseURL, path string, body any) (*envelope, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: http %d", ErrAPIError, resp.StatusCode)
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &env, nil
}

// DiscoverRegionAPI calls the global host with the user's email and country
// code, and returns the regional API base URL the account is bound to.
//
// In production, baseURL is GlobalAPIBase. Tests pass an httptest server URL.
func DiscoverRegionAPI(ctx context.Context, baseURL, email, userArea string, opts ...AuthOption) (string, error) {
	c := newAuthConfig(opts...)
	body := map[string]string{"username": email, "user_area": userArea}

	env, err := postJSON(ctx, c.httpClient, baseURL, "/auth/otp-send-code", body)
	if err != nil {
		return "", err
	}
	if err := env.toError(); err != nil {
		return "", err
	}

	if len(env.Data) == 0 {
		return "", fmt.Errorf("region discovery: missing data field")
	}
	var data struct {
		Domains struct {
			API string `json:"api"`
		} `json:"domains"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return "", fmt.Errorf("decoding data field: %w", err)
	}
	if data.Domains.API == "" {
		return "", fmt.Errorf("region discovery: empty data.domains.api")
	}
	return data.Domains.API, nil
}

// SendOTP requests a 6-digit code be emailed to the user and returns a
// short-lived exchange token to redeem with VerifyOTP.
func SendOTP(ctx context.Context, baseURL, email, userArea string, opts ...AuthOption) (string, error) {
	c := newAuthConfig(opts...)
	body := map[string]string{"username": email, "user_area": userArea}

	env, err := postJSON(ctx, c.httpClient, baseURL, "/auth/otp-send-code", body)
	if err != nil {
		return "", err
	}
	if err := env.toError(); err != nil {
		return "", err
	}
	if env.Token == "" {
		return "", fmt.Errorf("send OTP: missing token in response")
	}
	return env.Token, nil
}

// VerifyOTP redeems the exchange token + the 6-digit code from email, and
// returns the long-lived bearer access token. Returns ErrPasswordNotSet if
// the account has no password set (CLI surfaces a "set a password via web
// first" message rather than persisting an access_token that may not work
// for protected calls).
func VerifyOTP(ctx context.Context, baseURL, exchangeToken, code, userArea string, opts ...AuthOption) (string, error) {
	c := newAuthConfig(opts...)
	body := map[string]any{
		"token":                exchangeToken,
		"code":                 code,
		"user_area":            userArea,
		"require_set_password": true,
		"team_enabled":         false,
	}

	env, err := postJSON(ctx, c.httpClient, baseURL, "/auth/otp-login", body)
	if err != nil {
		return "", err
	}
	if err := env.toError(); err != nil {
		return "", err
	}

	if env.SetPasswordToken != "" {
		return "", fmt.Errorf("%w (set_password_token returned)", ErrPasswordNotSet)
	}
	if env.AccessToken == "" {
		return "", fmt.Errorf("verify OTP: missing access_token in response")
	}
	return env.AccessToken, nil
}
