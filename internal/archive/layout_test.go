package archive

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestPath_F03_FolderShape(t *testing.T) {
	root := t.TempDir()
	rec := Recording{
		ID:            "a3f9c021b2d34e5f6789012345678901",
		Title:         "Kickoff Meeting",
		TitleSlug:     "kickoff_meeting",
		RecordedAtUTC: time.Date(2026, 4, 30, 14, 30, 0, 0, time.UTC),
	}
	got, err := RecordingFolder(root, rec)
	if err != nil {
		t.Fatalf("RecordingFolder: %v", err)
	}
	want := filepath.Join(root, "2026", "04", "2026-04-30_1430_kickoff_meeting")
	if got != want {
		t.Fatalf("RecordingFolder = %q, want %q", got, want)
	}
}

func TestPath_F02_AutoCreatesArchiveRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "PlaudArchive")
	created, err := EnsureRoot(root)
	if err != nil {
		t.Fatalf("EnsureRoot: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true on first call")
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("root not created: %v", err)
	}
	// Second call: directory already exists, created=false.
	created, err = EnsureRoot(root)
	if err != nil {
		t.Fatalf("EnsureRoot 2: %v", err)
	}
	if created {
		t.Fatalf("expected created=false on second call")
	}
}

func TestPath_F02_OutFlagReplacesRootOnly(t *testing.T) {
	parent := t.TempDir()
	out := filepath.Join(parent, "custom")
	rec := Recording{
		ID:            "a3f9c021b2d34e5f6789012345678901",
		Title:         "Kickoff Meeting",
		TitleSlug:     "kickoff_meeting",
		RecordedAtUTC: time.Date(2026, 4, 30, 14, 30, 0, 0, time.UTC),
	}
	got, err := RecordingFolder(out, rec)
	if err != nil {
		t.Fatalf("RecordingFolder: %v", err)
	}
	want := filepath.Join(out, "2026", "04", "2026-04-30_1430_kickoff_meeting")
	if got != want {
		t.Fatalf("RecordingFolder = %q, want %q", got, want)
	}
}

func TestPath_F02_RejectsOutPointingAtFile(t *testing.T) {
	parent := t.TempDir()
	asFile := filepath.Join(parent, "iam_a_file")
	if err := os.WriteFile(asFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	_, err := EnsureRoot(asFile)
	if err == nil {
		t.Fatalf("EnsureRoot on a file did not error")
	}
	if !errors.Is(err, ErrPathNotDirectory) {
		t.Fatalf("err = %v, want ErrPathNotDirectory", err)
	}
}

func TestPath_F02_ProbesWritePermissionEarly(t *testing.T) {
	dir := t.TempDir()
	// First, the happy path: the dir is writable, ProbeWritable returns nil
	// and leaves no sentinel behind.
	if err := ProbeWritable(dir); err != nil {
		t.Fatalf("ProbeWritable on writable dir: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("ProbeWritable left files behind: %v", entries)
	}

	// Read-only dir: write should fail. Skipping on Windows where chmod
	// semantics differ.
	if runtime.GOOS == "windows" {
		t.Skip("skipping read-only chmod check on windows")
	}
	ro := filepath.Join(dir, "readonly")
	if err := os.MkdirAll(ro, 0o555); err != nil {
		t.Fatalf("mkdir readonly: %v", err)
	}
	defer os.Chmod(ro, 0o755)
	if err := ProbeWritable(ro); err == nil {
		t.Fatalf("ProbeWritable on read-only dir returned nil")
	}
}
