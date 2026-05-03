//go:build windows

package sync

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// Lock is the per-cycle exclusive write lock on the lock file (F-11).
// On Windows, implemented via golang.org/x/sys/windows.LockFileEx; the
// kernel auto-releases on process death.
type Lock struct {
	path string
	f    *os.File
}

// AcquireLock takes the lock under root. Returns ErrLocked when the lock
// is already held; the second return value carries holder info so the
// caller can render the contention message.
func AcquireLock(root string) (*Lock, *LockHolder, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, nil, fmt.Errorf("creating lock parent: %w", err)
	}
	path := filepath.Join(root, lockFilename)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("opening lock file: %w", err)
	}
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY)
	var ol windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, &ol); err != nil {
		holder, _ := readHolderFile(path)
		_ = f.Close()
		return nil, holder, ErrLocked
	}

	me := currentHolder()
	if err := writeHolderToFD(f, me); err != nil {
		var ol2 windows.Overlapped
		_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ol2)
		_ = f.Close()
		return nil, nil, fmt.Errorf("writing holder info: %w", err)
	}
	return &Lock{path: path, f: f}, me, nil
}

// Release unlocks and closes the lock file.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	var ol windows.Overlapped
	_ = windows.UnlockFileEx(windows.Handle(l.f.Fd()), 0, 1, 0, &ol)
	err := l.f.Close()
	l.f = nil
	return err
}
