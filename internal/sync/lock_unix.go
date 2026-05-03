//go:build unix

package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Lock is the per-cycle exclusive write lock on the state file's vicinity
// (F-11). On unix, implemented via syscall.Flock; the kernel auto-releases
// on process death.
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
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		holder, _ := readHolderFile(path)
		_ = f.Close()
		return nil, holder, ErrLocked
	}

	me := currentHolder()
	if err := writeHolderToFD(f, me); err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
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
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	err := l.f.Close()
	l.f = nil
	return err
}
