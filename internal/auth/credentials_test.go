package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// fixtureCreds returns a Credentials with non-empty values for round-trip tests.
func fixtureCreds() Credentials {
	return Credentials{
		Token:      "eyJ-fixture-token-xyz",
		Region:     "eu",
		Email:      "user@example.com",
		ObtainedAt: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}
}

// withTempConfig points credential lookup at a per-test temp directory by
// setting whichever env var the OS needs. Returns the resolved credentials
// path so tests can stat or read it.
func withTempConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", dir)
	} else {
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
	path, err := defaultPath()
	if err != nil {
		t.Fatalf("defaultPath: %v", err)
	}
	return path
}

// TestCredentials_F04_RoundTrip pins that Save followed by Load returns an
// equal Credentials value. This is the most fundamental contract of the
// package.
//
// Spec: specs/0001-auth-and-list/ F-04
func TestCredentials_F04_RoundTrip(t *testing.T) {
	withTempConfig(t)

	want := fixtureCreds()
	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.Token != want.Token {
		t.Errorf("Token: got %q, want %q", got.Token, want.Token)
	}
	if got.Region != want.Region {
		t.Errorf("Region: got %q, want %q", got.Region, want.Region)
	}
	if got.Email != want.Email {
		t.Errorf("Email: got %q, want %q", got.Email, want.Email)
	}
	if !got.ObtainedAt.Equal(want.ObtainedAt) {
		t.Errorf("ObtainedAt: got %v, want %v", got.ObtainedAt, want.ObtainedAt)
	}
}

// TestCredentials_F04_File0600OnPOSIX asserts that the on-disk credentials
// file is mode 0600 on POSIX systems. Windows uses ACLs and is not covered
// by this test.
//
// Spec: specs/0001-auth-and-list/ F-04
func TestCredentials_F04_File0600OnPOSIX(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode 0600 is a POSIX concept; Windows uses ACLs")
	}
	path := withTempConfig(t)

	if err := Save(fixtureCreds()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Errorf("file mode = %v, want %v", got, want)
	}
}

// TestCredentials_F05_FileShape asserts the on-disk JSON shape exactly: the
// four expected fields are present, no password is ever stored, and no
// additional surprise fields leak through.
//
// Spec: specs/0001-auth-and-list/ F-05
func TestCredentials_F05_FileShape(t *testing.T) {
	path := withTempConfig(t)

	if err := Save(fixtureCreds()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	wantKeys := map[string]bool{"token": true, "region": true, "email": true, "obtained_at": true}
	for k := range generic {
		if !wantKeys[k] {
			t.Errorf("unexpected field %q in credentials.json", k)
		}
		delete(wantKeys, k)
	}
	for k := range wantKeys {
		t.Errorf("missing field %q in credentials.json", k)
	}

	if _, hasPassword := generic["password"]; hasPassword {
		t.Error("credentials.json must never contain a password field")
	}
}

// TestCredentials_F07_MissingFileReturnsErrNotLoggedIn asserts that Load on
// a fresh config dir returns the typed sentinel callers can pattern-match,
// powering the "Not logged in. Run `plaud login` first." CLI message.
//
// Spec: specs/0001-auth-and-list/ F-07
func TestCredentials_F07_MissingFileReturnsErrNotLoggedIn(t *testing.T) {
	withTempConfig(t)

	_, err := Load()
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("Load on missing file: err = %v, want errors.Is ErrNotLoggedIn", err)
	}
}

// TestCredentials_DeleteIsIdempotent makes Delete safe to call by `plaud
// logout` regardless of prior state. F-AUTH-08 says logout deletes the
// stored token; calling it twice should not error.
//
// Spec: specs/0001-auth-and-list/ F-AUTH-08 (logout idempotency)
func TestCredentials_DeleteIsIdempotent(t *testing.T) {
	withTempConfig(t)

	if err := Save(fixtureCreds()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Delete(); err != nil {
		t.Fatalf("first Delete: %v", err)
	}
	if err := Delete(); err != nil {
		t.Fatalf("second Delete (should be idempotent): %v", err)
	}
}

// TestCredentials_TokenNotInErrorStrings makes sure that even when Load
// fails in error paths (corrupt JSON, etc.), the returned error does not
// contain the raw token. Tokens leaking into log output is a real risk on
// shared terminals; this is a defense-in-depth check.
//
// Spec: specs/0001-auth-and-list/ F-09
func TestCredentials_TokenNotInErrorStrings(t *testing.T) {
	path := withTempConfig(t)

	const secret = "tok-DO-NOT-LEAK-1234"
	corrupt := []byte(`{"token":"` + secret + `","region":"eu",` /* unterminated */)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := Load()
	if err == nil {
		t.Fatal("Load on corrupt JSON returned nil error, want non-nil")
	}
	if got := err.Error(); contains(got, secret) {
		t.Errorf("error string leaked token: %q", got)
	}
}

// contains is a tiny shim to avoid importing strings in a test file that
// otherwise has no use for it.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
