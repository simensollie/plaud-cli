package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestRoot_F11_HelpStatesUnofficial asserts that the root command's help text
// identifies the tool as unofficial and not affiliated with PLAUD LLC.
//
// Spec: specs/0001-auth-and-list/ F-11
func TestRoot_F11_HelpStatesUnofficial(t *testing.T) {
	cmd := newRootCmd()

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("executing --help: %v", err)
	}

	help := out.String()

	wantSubstrings := []string{
		"unofficial",
		"PLAUD LLC",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(strings.ToLower(help), strings.ToLower(want)) {
			t.Errorf("--help output missing required disclaimer fragment %q\n--- got ---\n%s", want, help)
		}
	}
}

// TestRoot_VersionFlag asserts that --version prints something non-empty.
//
// This is implementation hygiene rather than an FR, but it pins the version
// surface so future specs can rely on it.
func TestRoot_VersionFlag(t *testing.T) {
	cmd := newRootCmd()

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("executing --version: %v", err)
	}

	if got := strings.TrimSpace(out.String()); got == "" {
		t.Fatal("--version produced empty output")
	}
}
