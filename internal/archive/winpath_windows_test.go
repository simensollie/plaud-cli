//go:build windows

package archive

import (
	"path/filepath"
	"testing"
)

func TestWinPath_F18_LongPathPrefixOnWindows(t *testing.T) {
	got := PrefixLongPath(`C:\Users\foo\bar`)
	want := `\\?\C:\Users\foo\bar`
	if got != want {
		t.Fatalf("PrefixLongPath(C:\\Users\\foo\\bar) = %q, want %q", got, want)
	}
}

func TestWinPath_F18_AlreadyPrefixedReturnedUnchanged(t *testing.T) {
	in := `\\?\C:\foo`
	got := PrefixLongPath(in)
	if got != in {
		t.Fatalf("PrefixLongPath(%q) = %q, want unchanged", in, got)
	}
}

func TestWinPath_F18_UNCPathGetsUNCPrefix(t *testing.T) {
	got := PrefixLongPath(`\\server\share\foo`)
	want := `\\?\UNC\server\share\foo`
	if got != want {
		t.Fatalf("PrefixLongPath(\\\\server\\share\\foo) = %q, want %q", got, want)
	}
}

func TestWinPath_F18_RelativePathAbsolutizedFirst(t *testing.T) {
	in := `.\foo`
	abs, err := filepath.Abs(in)
	if err != nil {
		t.Fatalf("filepath.Abs(%q): %v", in, err)
	}
	want := `\\?\` + abs
	got := PrefixLongPath(in)
	if got != want {
		t.Fatalf("PrefixLongPath(%q) = %q, want %q", in, got, want)
	}
}
