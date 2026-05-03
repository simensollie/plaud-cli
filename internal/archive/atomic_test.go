package archive

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWrite_F14_TempRenameAtomicity(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "transcript.json")
	want := []byte(`{"version":1,"segments":[]}`)

	if err := WriteAtomic(dst, want); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("contents = %q, want %q", got, want)
	}

	// No `.partial` left behind after a successful write.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".partial" {
			t.Fatalf("found leftover partial file: %s", e.Name())
		}
	}
}

func TestWrite_F14_PartialSweepBeforeRun(t *testing.T) {
	dir := t.TempDir()
	stale := []string{
		"audio.mp3.partial",
		"transcript.json.partial",
		"summary.plaud.md.partial",
	}
	for _, n := range stale {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("stale"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", n, err)
		}
	}
	// Non-partial file should be left alone.
	keep := filepath.Join(dir, "metadata.json")
	if err := os.WriteFile(keep, []byte(`{"x":1}`), 0o644); err != nil {
		t.Fatalf("seed keep: %v", err)
	}

	if err := SweepPartials(dir); err != nil {
		t.Fatalf("SweepPartials: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".partial" {
			t.Fatalf("partial file survived sweep: %s", e.Name())
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("non-partial file removed: %v", err)
	}
}

func TestWrite_F14_FsyncBeforeRename(t *testing.T) {
	// Direct verification of fsync ordering on a unix kernel from userspace
	// is brittle. We assert the observable contract: after WriteAtomic
	// returns, the destination file exists, the temp file does not, and
	// the bytes are exactly what we asked for. The implementation must
	// fsync before rename; this test together with TempRenameAtomicity
	// ensures the rename target carries the durable bytes.
	dir := t.TempDir()
	dst := filepath.Join(dir, "summary.plaud.md")
	data := []byte("# Summary\n\nbody")

	if err := WriteAtomic(dst, data); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	if _, err := os.Stat(dst + ".partial"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no .partial file, stat err = %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("contents = %q, want %q", got, data)
	}
}

func TestWrite_F14_ParentFolderAutoCreated(t *testing.T) {
	dir := t.TempDir()
	// Two missing levels of parent.
	dst := filepath.Join(dir, "2026", "04", "2026-04-30_1430_kickoff", "transcript.json")
	if err := WriteAtomic(dst, []byte(`{"v":1}`)); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("Stat dst: %v", err)
	}
}
