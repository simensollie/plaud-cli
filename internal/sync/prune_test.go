package sync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simensollie/plaud-cli/internal/api"
)

func mkFolder(t *testing.T, root, rel string) string {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(abs, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", abs, err)
	}
	if err := os.WriteFile(filepath.Join(abs, "metadata.json"), []byte(`{"id":"x"}`), 0o644); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	return abs
}

func TestPrune_F09_MovesToTrash(t *testing.T) {
	root := t.TempDir()
	rel := "2026/04/2026-04-30_1430_kickoff"
	src := mkFolder(t, root, rel)

	p := &FolderPruner{Now: func() time.Time { return time.Date(2026, 5, 3, 14, 30, 0, 0, time.UTC) }}
	act := Action{
		Kind:        ActionPrune,
		Recording:   api.Recording{ID: "abc123"},
		OldRelative: rel,
	}
	got, err := p.Prune(context.Background(), root, act)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}

	// Source should be gone.
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("src should be gone, stat err=%v", err)
	}
	// Target should be at .trash/abc123/
	wantTrash := filepath.Join(root, ".trash", "abc123")
	if got.TrashPath != wantTrash {
		t.Errorf("TrashPath=%q, want %q", got.TrashPath, wantTrash)
	}
	if _, err := os.Stat(filepath.Join(wantTrash, "metadata.json")); err != nil {
		t.Errorf("trash metadata missing: %v", err)
	}
}

func TestPrune_F09_CollisionAppendsTimestampSuffix(t *testing.T) {
	root := t.TempDir()
	id := "abc123"
	preexisting := filepath.Join(root, ".trash", id)
	if err := os.MkdirAll(preexisting, 0o755); err != nil {
		t.Fatalf("seed pre-existing trash: %v", err)
	}
	if err := os.WriteFile(filepath.Join(preexisting, "marker.txt"), []byte("preserve me"), 0o644); err != nil {
		t.Fatalf("seed marker: %v", err)
	}

	rel := "2026/04/2026-04-30_1430_kickoff"
	mkFolder(t, root, rel)

	now := time.Date(2026, 5, 3, 14, 30, 0, 0, time.UTC)
	p := &FolderPruner{Now: func() time.Time { return now }}
	act := Action{Kind: ActionPrune, Recording: api.Recording{ID: id}, OldRelative: rel}
	got, err := p.Prune(context.Background(), root, act)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}

	if !strings.Contains(got.TrashPath, "abc123__") {
		t.Errorf("expected double-underscore suffix in %q", got.TrashPath)
	}
	// Pre-existing .trash/abc123/marker.txt should be untouched.
	if _, err := os.Stat(filepath.Join(preexisting, "marker.txt")); err != nil {
		t.Errorf("pre-existing trash content disturbed: %v", err)
	}
}

func TestPrune_F09_DoesNotOverwriteExistingTrash(t *testing.T) {
	root := t.TempDir()
	id := "deadbeef"
	pre := filepath.Join(root, ".trash", id)
	if err := os.MkdirAll(pre, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	preFile := filepath.Join(pre, "important.txt")
	want := []byte("do not overwrite me")
	if err := os.WriteFile(preFile, want, 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	rel := "2026/04/folder"
	mkFolder(t, root, rel)

	p := &FolderPruner{Now: func() time.Time { return time.Date(2026, 5, 3, 14, 30, 0, 0, time.UTC) }}
	act := Action{Kind: ActionPrune, Recording: api.Recording{ID: id}, OldRelative: rel}
	if _, err := p.Prune(context.Background(), root, act); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	got, err := os.ReadFile(preFile)
	if err != nil {
		t.Fatalf("reading preserved file: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("pre-existing trash content was overwritten")
	}
}

func TestPrune_F09_MissingSourceFolderIsTolerated(t *testing.T) {
	// The reconciler may emit a prune action for a state entry whose folder
	// has already been removed manually. Don't error; just record the
	// trash path that would have been used.
	root := t.TempDir()
	p := &FolderPruner{Now: func() time.Time { return time.Date(2026, 5, 3, 14, 30, 0, 0, time.UTC) }}
	act := Action{Kind: ActionPrune, Recording: api.Recording{ID: "ghost"}, OldRelative: "2026/04/folder-not-here"}
	_, err := p.Prune(context.Background(), root, act)
	if err != nil {
		t.Fatalf("Prune (missing src): %v", err)
	}
}

func TestPrune_F09_EmptyIDRefuses(t *testing.T) {
	root := t.TempDir()
	p := &FolderPruner{Now: time.Now}
	_, err := p.Prune(context.Background(), root, Action{Kind: ActionPrune, OldRelative: "x"})
	if err == nil {
		t.Fatal("expected error on empty ID")
	}
}
