package sync

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
)

// WatchSentinel is the advisory "another watcher is here" file. Acquiring
// the sentinel writes our PID/host/started-at; releasing removes the file.
// Acquisition fails with ErrWatchActive when another watcher's PID on the
// same host is alive (F-11). Cross-host conflicts are reported but the
// caller has to decide whether to proceed.
type WatchSentinel struct {
	path string
}

// AcquireWatchSentinel writes the sentinel file under root. Returns
// ErrWatchActive (and the holder) when an active watcher already exists.
// Stale-watcher recovery: same-host PID not running → take the sentinel.
func AcquireWatchSentinel(root string) (*WatchSentinel, *LockHolder, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, nil, err
	}
	path := filepath.Join(root, watchSentinelFilename)

	if existing, ok := readHolderFile(path); ok {
		hostMatches := existing.Hostname == ""
		if !hostMatches {
			h, _ := os.Hostname()
			hostMatches = existing.Hostname == h
		}
		if hostMatches && !pidAlive(existing.PID) {
			// Stale: take it over.
		} else {
			return nil, existing, ErrWatchActive
		}
	}

	me := currentHolder()
	if err := writeHolderFile(path, me); err != nil {
		return nil, nil, err
	}
	return &WatchSentinel{path: path}, me, nil
}

// Release removes the sentinel file. Idempotent; missing file is OK.
func (w *WatchSentinel) Release() error {
	if w == nil || w.path == "" {
		return nil
	}
	if err := os.Remove(w.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// pidAlive reports whether a process with the given PID exists. On Unix,
// signal 0 is the standard probe. On Windows, os.FindProcess always
// succeeds, so we look at the actual exit signal differently — runtime
// fallback returns true (assume alive) so the cautious branch wins.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		// FindProcess on Windows returns a handle even for dead PIDs; we
		// don't have a portable cheap check that doesn't pull x/sys deeper.
		// Conservatively assume alive — the operator can manually delete
		// the sentinel file if they're certain.
		return true
	}
	// Unix: signal 0 doesn't deliver, just probes.
	if err := p.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	return true
}
