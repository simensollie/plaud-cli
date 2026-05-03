package archive

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrPathNotDirectory indicates a target path that should be (or become) a
// directory exists as a regular file. Surfaced before any network call.
// F-02.
var ErrPathNotDirectory = errors.New("path exists and is not a directory")

const sentinelName = ".plaud_writable"

// RecordingFolder returns the absolute folder path for a recording under
// the given archive root. Layout: `<root>/YYYY/MM/YYYY-MM-DD_HHMM_<slug>/`
// in UTC. F-03.
func RecordingFolder(root string, r Recording) (string, error) {
	if r.TitleSlug == "" {
		return "", fmt.Errorf("recording has empty TitleSlug")
	}
	t := r.RecordedAtUTC.UTC()
	year := fmt.Sprintf("%04d", t.Year())
	month := fmt.Sprintf("%02d", int(t.Month()))
	leaf := fmt.Sprintf("%s-%s-%02d_%02d%02d_%s",
		year, month, t.Day(), t.Hour(), t.Minute(), r.TitleSlug)
	return filepath.Join(root, year, month, leaf), nil
}

// EnsureRoot creates the archive root if missing. Returns created=true
// only on the first creation, so callers can emit a one-line stderr notice.
// Errors with ErrPathNotDirectory if root exists as a regular file. F-02.
func EnsureRoot(root string) (created bool, err error) {
	info, statErr := os.Stat(root)
	if statErr == nil {
		if !info.IsDir() {
			return false, fmt.Errorf("%w: %s", ErrPathNotDirectory, root)
		}
		return false, nil
	}
	if !errors.Is(statErr, os.ErrNotExist) {
		return false, fmt.Errorf("statting %s: %w", root, statErr)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return false, fmt.Errorf("creating %s: %w", root, err)
	}
	return true, nil
}

// ProbeWritable verifies the caller can create and remove a file inside
// dir. Returns nil on success; an actionable error on failure. Run before
// any network call so we fail fast on permission problems. F-02.
func ProbeWritable(dir string) error {
	p := filepath.Join(dir, sentinelName)
	f, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("probing write access to %s: %w", dir, err)
	}
	if _, err := f.Write([]byte("x")); err != nil {
		_ = f.Close()
		_ = os.Remove(p)
		return fmt.Errorf("probing write access to %s: %w", dir, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(p)
		return fmt.Errorf("probing write access to %s: %w", dir, err)
	}
	if err := os.Remove(p); err != nil {
		return fmt.Errorf("removing sentinel %s: %w", p, err)
	}
	return nil
}
