package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/simensollie/plaud-cli/internal/auth"
)

// runLogoutCmd executes the logout subcommand and returns combined output
// and any error.
func runLogoutCmd(t *testing.T) (string, error) {
	t.Helper()
	cmd := newLogoutCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())
	err := cmd.Execute()
	return buf.String(), err
}

// TestLogout_DeletesCredentialsFile asserts the basic happy path: when
// credentials exist, logout removes them and leaves auth.Load returning
// ErrNotLoggedIn.
//
// Spec: specs/0001-auth-and-list/ F-AUTH-08
func TestLogout_DeletesCredentialsFile(t *testing.T) {
	setTempConfig(t)

	if err := auth.Save(auth.Credentials{
		Token: "tok", Region: "eu", Email: "u@example.com", ObtainedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}

	out, err := runLogoutCmd(t)
	if err != nil {
		t.Fatalf("logout: %v\n%s", err, out)
	}

	if _, loadErr := auth.Load(); !errors.Is(loadErr, auth.ErrNotLoggedIn) {
		t.Errorf("after logout, auth.Load err = %v, want ErrNotLoggedIn", loadErr)
	}

	if !strings.Contains(strings.ToLower(out), "logged out") {
		t.Errorf("expected confirmation message containing \"logged out\", got: %q", out)
	}
}

// TestLogout_IdempotentWhenAlreadyLoggedOut asserts that a second logout
// (with no credentials present) is a quiet success, not an error. Powers
// the sane-by-default behavior expected by users invoking logout in
// scripts or after-the-fact cleanup.
//
// Spec: specs/0001-auth-and-list/ F-AUTH-08 (idempotency)
func TestLogout_IdempotentWhenAlreadyLoggedOut(t *testing.T) {
	setTempConfig(t)

	if _, err := runLogoutCmd(t); err != nil {
		t.Fatalf("first logout (no creds): %v", err)
	}
	if _, err := runLogoutCmd(t); err != nil {
		t.Fatalf("second logout (still no creds): %v", err)
	}
}
