// Package sync owns the spec 0003 sync runner: state file, reconciliation,
// concurrency, NDJSON events, and the per-cycle write lock. The per-recording
// fetch primitive lives in internal/fetch (extracted in plan Phase 0a).
package sync

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/simensollie/plaud-cli/internal/api"
)

// stateFilename is the on-disk name of the sync state file. F-03.
const stateFilename = ".plaud-sync.state"

// SchemaVersion bumps only on breaking changes to the state-file shape.
const SchemaVersion = 1

// State is the in-memory representation of <archive_root>/.plaud-sync.state.
// Acts as a minimal index; per-recording artifact truth (hashes, sizes,
// presence) lives in each recording's metadata.json (spec 0002 §4).
type State struct {
	SchemaVersion   int                       `json:"schema_version"`
	LastRunStarted  time.Time                 `json:"last_run_started"`
	LastRunFinished time.Time                 `json:"last_run_finished"`
	Recordings      map[string]RecordingState `json:"recordings"`
}

// RecordingState is the per-recording entry. Version (and VersionMs) are the
// staleness signal for F-15 Layer B; FolderPath is the rename pointer
// (relative to archive root) for F-16; LastError records the last failure
// for surfacing across runs.
type RecordingState struct {
	Version    string          `json:"version"`
	VersionMs  int64           `json:"version_ms"`
	FolderPath string          `json:"folder_path"`
	LastError  *RecordingError `json:"last_error,omitempty"`
}

// RecordingError captures the most recent per-recording failure. Msg is
// redacted at Save time (F-13).
type RecordingError struct {
	Msg string    `json:"msg"`
	At  time.Time `json:"at"`
}

// ErrFolderPathAbsolute is returned by Save when a state entry's folder
// path is absolute. The state file uses paths relative to the archive root
// so that --out DIR overrides do not break rename detection (F-03, F-16).
var ErrFolderPathAbsolute = errors.New("recording folder_path must be relative to archive root")

// statePath returns the absolute path of the state file under root.
func statePath(root string) string {
	return filepath.Join(root, stateFilename)
}

// Load reads the state file. A missing file returns a fresh state at the
// current schema version (F-03: deleting .plaud-sync.state is a safe
// recovery). Stale .tmp files left by a crashed Save are ignored — Load
// reads only the canonical filename.
func Load(root string) (*State, error) {
	raw, err := os.ReadFile(statePath(root))
	if errors.Is(err, os.ErrNotExist) {
		return freshState(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading state file: %w", err)
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("decoding state file: %w", err)
	}
	if s.SchemaVersion == 0 {
		s.SchemaVersion = SchemaVersion
	}
	if s.Recordings == nil {
		s.Recordings = map[string]RecordingState{}
	}
	return &s, nil
}

// Save writes s to the state file atomically (tmp+rename). Validates that
// every folder_path is relative; redacts every last_error.msg via
// api.RedactString before serialization (F-13).
func Save(root string, s *State) error {
	if s == nil {
		return errors.New("nil state")
	}
	out := *s
	if out.SchemaVersion == 0 {
		out.SchemaVersion = SchemaVersion
	}
	if out.Recordings == nil {
		out.Recordings = map[string]RecordingState{}
	}

	for id, rs := range out.Recordings {
		if filepath.IsAbs(rs.FolderPath) {
			return fmt.Errorf("%w: id=%s path=%q", ErrFolderPathAbsolute, id, rs.FolderPath)
		}
		// Normalize OS-native separators to forward slashes so the on-disk
		// shape is stable across platforms (Windows produces backslashes).
		rs.FolderPath = filepath.ToSlash(rs.FolderPath)
		if rs.LastError != nil {
			redacted := *rs.LastError
			redacted.Msg = api.RedactString(redacted.Msg)
			rs.LastError = &redacted
		}
		out.Recordings[id] = rs
	}

	raw, err := marshalState(&out)
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("creating state root: %w", err)
	}
	dst := statePath(root)
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("writing state tmp: %w", err)
	}
	// fsync the tmp before rename so a crash between write and rename leaves
	// a consistent file on disk; Load explicitly ignores .tmp regardless.
	if f, err := os.OpenFile(tmp, os.O_RDWR, 0o644); err == nil {
		_ = f.Sync()
		_ = f.Close()
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming state: %w", err)
	}
	return nil
}

// marshalState renders s as pretty-printed JSON with sorted keys and a
// trailing newline. Mirrors the metadata.json formatting for predictable
// diffs in archives that live under version control.
func marshalState(s *State) ([]byte, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(generic, "", "  ")
	if err != nil {
		return nil, err
	}
	out = append(out, '\n')
	return out, nil
}

// freshState constructs a zero-value State at the current schema version.
func freshState() *State {
	return &State{
		SchemaVersion: SchemaVersion,
		Recordings:    map[string]RecordingState{},
	}
}

// IsAbsolute reports whether p is an absolute path. Helper for callers that
// validate folder paths before stuffing them into State.
func IsAbsolute(p string) bool { return filepath.IsAbs(p) || strings.HasPrefix(p, "/") }
