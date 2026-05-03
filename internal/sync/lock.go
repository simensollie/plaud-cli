package sync

import (
	"encoding/json"
	"errors"
	"os"
	"time"
)

// lockFilename is the on-disk name of the per-cycle write lock. F-11.
const lockFilename = ".plaud-sync.lock"

// watchSentinelFilename is the advisory "a watch loop is here" file. Not a
// lock; just a presence marker that two-watch detection (F-11) reads.
const watchSentinelFilename = ".plaud-sync.watch"

// ErrLocked is returned by AcquireLock when another process holds the
// per-cycle lock. The caller renders the structured contention message
// from the LockHolder data.
var ErrLocked = errors.New("plaud-sync.lock is held by another process")

// ErrWatchActive is returned by AcquireWatchSentinel when another watcher
// already advertises itself in `.plaud-sync.watch` and the holder is alive.
var ErrWatchActive = errors.New("a plaud sync watch loop is already active")

// LockHolder records who holds the lock or watch sentinel. Persisted to
// the lock/sentinel file as JSON; read by the contention-message printer.
type LockHolder struct {
	PID       int       `json:"pid"`
	Hostname  string    `json:"hostname"`
	StartedAt time.Time `json:"started_at_utc"`
}

func readHolderFile(path string) (*LockHolder, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var h LockHolder
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, false
	}
	return &h, true
}

func writeHolderFile(path string, h *LockHolder) error {
	raw, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// writeHolderToFD writes the holder JSON directly to an already-open and
// already-flocked file descriptor. Renaming a sibling tmp file over the
// lock path would unlink the inode our flock is on, so the lock file
// itself must be updated in place.
func writeHolderToFD(f *os.File, h *LockHolder) error {
	raw, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	if _, err := f.Write(raw); err != nil {
		return err
	}
	return f.Sync()
}

func currentHolder() *LockHolder {
	h, _ := os.Hostname()
	return &LockHolder{PID: os.Getpid(), Hostname: h, StartedAt: time.Now().UTC()}
}
