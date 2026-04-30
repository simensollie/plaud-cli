package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/simensollie/plaud-cli/internal/api"
	"github.com/simensollie/plaud-cli/internal/auth"
)

// setTempConfig points credentials lookup at a per-test temp dir on every
// supported OS. Tests that exercise auth.Save / auth.Load must call this or
// the Save will write to the user's real config dir on Windows (which reads
// APPDATA, not XDG_CONFIG_HOME).
func setTempConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
}

// fakePlaud serves the two OTP endpoints with configurable responses. Test
// helper that keeps each test focused on its own assertions instead of HTTP
// scaffolding.
type fakePlaud struct {
	otpSendBody  string
	otpLoginBody string
	requestCount int
}

func (f *fakePlaud) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requestCount++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/otp-send-code":
			_, _ = io.WriteString(w, f.otpSendBody)
		case "/auth/otp-login":
			_, _ = io.WriteString(w, f.otpLoginBody)
		default:
			http.NotFound(w, r)
		}
	})
}

// runLoginCmd builds a login command with the resolver pinned to srv.URL,
// drives it with the given stdin, and returns combined output + any error.
func runLoginCmd(t *testing.T, srvURL, stdin string) (string, error) {
	t.Helper()
	cmd := newLoginCmd(withBaseURLResolver(func(_ api.Region) (string, error) {
		return srvURL, nil
	}))
	cmd.SetIn(strings.NewReader(stdin))
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())
	err := cmd.Execute()
	return buf.String(), err
}

// TestLogin_F01_F02_HappyPath drives the full OTP flow against a fake
// server, simulated stdin (region, email, code), and asserts the credentials
// file lands with the expected contents.
//
// Spec: specs/0001-auth-and-list/ F-01, F-02, F-04, F-05
func TestLogin_F01_F02_HappyPath(t *testing.T) {
	setTempConfig(t)
	t.Setenv("LANG", "nb_NO.UTF-8")

	const accessToken = "eyJ-real-access-token"
	fp := &fakePlaud{
		otpSendBody:  `{"status":0,"token":"exchange-abc"}`,
		otpLoginBody: `{"status":0,"access_token":"` + accessToken + `","token_type":"bearer","has_password":true,"is_new_user":false}`,
	}
	srv := httptest.NewServer(fp.handler())
	t.Cleanup(srv.Close)

	out, err := runLoginCmd(t, srv.URL, "eu\nuser@example.com\n123456\n")
	if err != nil {
		t.Fatalf("login failed: %v\noutput:\n%s", err, out)
	}

	creds, err := auth.Load()
	if err != nil {
		t.Fatalf("auth.Load after login: %v", err)
	}
	if creds.Token != accessToken {
		t.Errorf("Token: got %q, want %q", creds.Token, accessToken)
	}
	if creds.Region != "eu" {
		t.Errorf("Region: got %q, want %q", creds.Region, "eu")
	}
	if creds.Email != "user@example.com" {
		t.Errorf("Email: got %q, want %q", creds.Email, "user@example.com")
	}
	if creds.ObtainedAt.IsZero() {
		t.Error("ObtainedAt is zero, want set")
	}
}

// TestLogin_F02_BadCodeExitsNonZero asserts that a server-rejected OTP
// surfaces as a non-nil error from cmd.Execute, so the CLI exits non-zero.
//
// Spec: specs/0001-auth-and-list/ F-02
func TestLogin_F02_BadCodeExitsNonZero(t *testing.T) {
	setTempConfig(t)

	fp := &fakePlaud{
		otpSendBody:  `{"status":0,"token":"exchange-abc"}`,
		otpLoginBody: `{"status":40010,"msg":"invalid otp code"}`,
	}
	srv := httptest.NewServer(fp.handler())
	t.Cleanup(srv.Close)

	_, err := runLoginCmd(t, srv.URL, "eu\nuser@example.com\n000000\n")
	if err == nil {
		t.Fatal("login with bad code returned nil error, want non-nil")
	}

	if _, loadErr := auth.Load(); loadErr == nil {
		t.Error("credentials saved despite failed login; expected ErrNotLoggedIn")
	}
}

// TestLogin_F02_PasswordNotSetIsActionableMessage asserts that the
// CLI translates the API's "no password set" signal into a clear,
// pointed message rather than dumping a raw error.
//
// Spec: specs/0001-auth-and-list/ F-02 (set-password edge case)
func TestLogin_F02_PasswordNotSetIsActionableMessage(t *testing.T) {
	setTempConfig(t)

	fp := &fakePlaud{
		otpSendBody:  `{"status":0,"token":"exchange-abc"}`,
		otpLoginBody: `{"status":0,"access_token":"x","has_password":false,"is_new_user":true,"set_password_token":"set-pw"}`,
	}
	srv := httptest.NewServer(fp.handler())
	t.Cleanup(srv.Close)

	out, err := runLoginCmd(t, srv.URL, "eu\nuser@example.com\n123456\n")
	if err == nil {
		t.Fatal("login with password-not-set returned nil error, want non-nil")
	}
	combined := out + err.Error()
	for _, want := range []string{"password", "web.plaud.ai"} {
		if !strings.Contains(strings.ToLower(combined), strings.ToLower(want)) {
			t.Errorf("error/output missing %q to be actionable\n--- combined ---\n%s", want, combined)
		}
	}
}

// TestLogin_F09_TokenNeverInOutput asserts the bearer access_token never
// surfaces in stdout, stderr, or the returned error string. Spec F-09 says
// tokens, OTP codes, and Authorization headers must never be logged.
//
// Spec: specs/0001-auth-and-list/ F-09
func TestLogin_F09_TokenNeverInOutput(t *testing.T) {
	setTempConfig(t)

	const secret = "tok-DO-NOT-LEAK-eyJ-9999"
	fp := &fakePlaud{
		otpSendBody:  `{"status":0,"token":"exchange-abc"}`,
		otpLoginBody: `{"status":0,"access_token":"` + secret + `","has_password":true}`,
	}
	srv := httptest.NewServer(fp.handler())
	t.Cleanup(srv.Close)

	out, err := runLoginCmd(t, srv.URL, "eu\nuser@example.com\n123456\n")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if strings.Contains(out, secret) {
		t.Errorf("output leaked bearer token:\n%s", out)
	}
	// Also check the OTP code does not get echoed in cleartext.
	if strings.Contains(out, "123456") {
		t.Errorf("output leaked OTP code:\n%s", out)
	}
}

// TestDetectUserArea_FromLANG pins the $LANG → ISO 3166-1 alpha-2 mapping.
//
// Spec: specs/0001-auth-and-list/ F-02 (user_area derivation)
func TestDetectUserArea_FromLANG(t *testing.T) {
	cases := []struct {
		name string
		lang string
		want string
	}{
		{"norwegian", "nb_NO.UTF-8", "NO"},
		{"american", "en_US.UTF-8", "US"},
		{"japanese", "ja_JP.UTF-8", "JP"},
		{"british", "en_GB.UTF-8", "GB"},
		{"empty", "", "US"},
		{"only-language", "en", "US"},
		{"with-modifier", "ca_ES@valencia", "ES"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectUserArea(tc.lang, ""); got != tc.want {
				t.Errorf("detectUserArea(%q) = %q, want %q", tc.lang, got, tc.want)
			}
		})
	}
}
