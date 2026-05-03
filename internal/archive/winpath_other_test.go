//go:build !windows

package archive

import "testing"

func TestWinPath_F18_NoOpOnPOSIX(t *testing.T) {
	in := "/home/user/foo"
	got := PrefixLongPath(in)
	if got != in {
		t.Fatalf("PrefixLongPath(%q) = %q, want unchanged", in, got)
	}
}

func TestWinPath_F18_NoOpOnPOSIXWithRelative(t *testing.T) {
	in := "./foo"
	got := PrefixLongPath(in)
	if got != in {
		t.Fatalf("PrefixLongPath(%q) = %q, want unchanged", in, got)
	}
}
