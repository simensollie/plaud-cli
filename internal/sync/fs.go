package sync

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/simensollie/plaud-cli/internal/archive"
)

// OSFilesystem is a Filesystem implementation that reads metadata.json
// from a real archive root. Used by the runner; tests use fakeFS.
type OSFilesystem struct {
	Root string
}

// LoadMetadata reads <root>/<rel>/metadata.json, returning nil for
// missing or unparseable files. The reconciler interprets nil as "folder
// is missing locally; re-fetch".
func (fs *OSFilesystem) LoadMetadata(rel string) (*archive.Metadata, error) {
	if rel == "" {
		return nil, nil
	}
	abs := filepath.Join(fs.Root, filepath.FromSlash(rel))
	raw, err := os.ReadFile(filepath.Join(abs, archive.MetadataFilename))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m, err := archive.UnmarshalMetadata(raw)
	if err != nil {
		return nil, nil // corrupt → reconciler treats as absent
	}
	return m, nil
}
