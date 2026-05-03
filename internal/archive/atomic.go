package archive

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const partialSuffix = ".partial"

// WriteAtomic writes data to path via a sibling `.partial` file, fsyncs the
// bytes to disk, then renames over the destination. The parent directory is
// created recursively if missing. F-14.
func WriteAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating parent dir: %w", err)
	}

	tmp := path + partialSuffix
	f, err := os.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("opening %s: %w", tmp, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("fsync %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("closing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming %s to %s: %w", tmp, path, err)
	}
	return nil
}

// SweepPartials removes every `*.partial` file directly inside folder. Run
// at the start of each download run so a previously-aborted write does not
// confuse an idempotency check. F-14.
func SweepPartials(folder string) error {
	entries, err := os.ReadDir(folder)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading %s: %w", folder, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), partialSuffix) {
			continue
		}
		p := filepath.Join(folder, e.Name())
		if err := os.Remove(p); err != nil {
			return fmt.Errorf("removing %s: %w", p, err)
		}
	}
	return nil
}
