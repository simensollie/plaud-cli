package sync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// trashDir is the relative root under archive_root for pruned folders.
// F-09. The user is expected to clean it up periodically; we never touch
// existing entries.
const trashDir = ".trash"

// FolderPruner implements Pruner by moving the recording folder into
// `<root>/.trash/<id>/`. On collision (a previous prune of the same id),
// the new entry lands at `<root>/.trash/<id>__<UTC ISO 8601>/` so existing
// trash content stays untouched (F-09).
type FolderPruner struct {
	Now func() time.Time
}

// Prune moves the recording's folder into .trash/. Mass-deletion guards
// are enforced by the reconciler (Reconcile / ErrMassDeletion); this
// pruner trusts that the action it receives is safe to execute.
func (p *FolderPruner) Prune(ctx context.Context, root string, action Action) (PruneResult, error) {
	if action.Recording.ID == "" {
		return PruneResult{}, errors.New("prune: empty recording id")
	}
	now := p.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}

	src := ""
	if action.OldRelative != "" {
		src = filepath.Join(root, filepath.FromSlash(action.OldRelative))
	}

	target := filepath.Join(root, trashDir, action.Recording.ID)
	if _, err := os.Stat(target); err == nil {
		// Collision: append __<UTC ISO 8601> (no colons; filesystem-safe).
		ts := now().UTC().Format("2006-01-02T150405Z")
		target = filepath.Join(root, trashDir, action.Recording.ID+"__"+ts)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return PruneResult{}, fmt.Errorf("creating .trash parent: %w", err)
	}

	// Tolerate missing source: the reconciler may schedule a prune for a
	// recording whose folder has already been removed manually. The state
	// entry still needs to be cleared (the runner does that after we return).
	if src != "" {
		if _, err := os.Stat(src); err == nil {
			if err := os.Rename(src, target); err != nil {
				return PruneResult{}, fmt.Errorf("moving %s to %s: %w", src, target, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return PruneResult{}, fmt.Errorf("statting %s: %w", src, err)
		}
	}

	_ = ctx // ctx kept for future extensions (e.g. cancellable IO)
	return PruneResult{TrashPath: target}, nil
}
