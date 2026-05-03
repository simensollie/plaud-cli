package sync

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/simensollie/plaud-cli/internal/api"
	"github.com/simensollie/plaud-cli/internal/archive"
)

// ActionKind enumerates the per-recording decisions Reconcile produces.
type ActionKind string

const (
	ActionFetch  ActionKind = "fetch"
	ActionSkip   ActionKind = "skip"
	ActionPrune  ActionKind = "prune"
	ActionRename ActionKind = "rename"
)

// Reason classifies why an action was scheduled. Stable strings; emitters
// (NDJSON event details) match against them.
type Reason string

const (
	ReasonNew             Reason = "new"
	ReasonPresenceFlip    Reason = "presence-flip"
	ReasonVersionBump     Reason = "version-bump"
	ReasonMissingLocally  Reason = "missing-locally"
	ReasonForce           Reason = "force"
	ReasonRename          Reason = "rename"
	ReasonDeletedOnServer Reason = "deleted-on-server"
	ReasonVerified        Reason = "verified"
)

// Action is one per-recording decision. For Rename, the runner performs the
// folder move and then runs FetchOne against the new path (FetchOne's
// per-artifact idempotency handles the post-rename state).
type Action struct {
	Kind      ActionKind
	Recording api.Recording
	Reason    Reason
	// OldRelative is the current state.recordings[id].folder_path. Set for
	// Rename and Prune.
	OldRelative string
	// NewRelative is the destination relative path. Set for Rename.
	NewRelative string
	// Folder is the relative path the runner should fetch into. For Fetch
	// and Skip actions, this is the canonical path computed from the current
	// list response.
	Folder string
}

// ReconcileOptions configures one reconciliation pass.
type ReconcileOptions struct {
	Include            archive.IncludeSet
	Prune              bool
	IncludeTrashed     bool
	PruneEmpty         bool
	EnableVersionLayer bool
}

// Reconciliation is the result of one Reconcile call.
type Reconciliation struct {
	Actions []Action
}

// Filesystem reads per-recording metadata.json. Reconcile takes this as a
// dependency to stay a pure function over (lists, state, fs); the runner
// supplies a real implementation, tests provide an in-memory map.
type Filesystem interface {
	// LoadMetadata returns nil, nil when the metadata file is absent or
	// unparseable; nil, err for IO errors. Reconcile treats nil-metadata as
	// "folder is missing locally, re-fetch".
	LoadMetadata(relativeFolder string) (*archive.Metadata, error)
}

// ErrMassDeletion wraps mass-deletion guard refusals (F-09). Callers detect
// via errors.Is and surface the actionable --prune-empty bypass to the user.
var ErrMassDeletion = errors.New("refusing to prune: mass-deletion guard tripped")

// Reconcile decides what actions to take given the server-side enumeration
// (default + trashed list calls), the current sync state, and a filesystem
// view of the per-recording metadata files. Pure: no IO.
//
// listDefault is is_trash=0; listTrashed is is_trash=1 (may be nil when the
// caller did not enumerate, which is fine when --prune is off).
func Reconcile(
	listDefault []api.Recording,
	listTrashed []api.Recording,
	state *State,
	fs Filesystem,
	opts ReconcileOptions,
) (*Reconciliation, error) {
	if state == nil {
		state = freshState()
	}
	if fs == nil {
		fs = noopFS{}
	}

	out := &Reconciliation{}
	serverIDs := map[string]bool{}

	for _, rec := range listDefault {
		serverIDs[rec.ID] = true
		out.Actions = append(out.Actions, decideForRecording(rec, state, fs, opts))
	}

	for _, rec := range listTrashed {
		serverIDs[rec.ID] = true
		if !opts.IncludeTrashed {
			// Track the id (so prune doesn't kill its local folder), but
			// don't fetch.
			continue
		}
		// Force IsTrash flag so the fetch surfaces F-17 trash warning.
		rec.IsTrash = true
		out.Actions = append(out.Actions, decideForRecording(rec, state, fs, opts))
	}

	if opts.Prune {
		missing := []string{}
		for id := range state.Recordings {
			if !serverIDs[id] {
				missing = append(missing, id)
			}
		}
		if len(missing) > 0 {
			if err := checkMassDeletionGuards(missing, serverIDs, state, opts); err != nil {
				return nil, err
			}
			for _, id := range missing {
				rs := state.Recordings[id]
				out.Actions = append(out.Actions, Action{
					Kind:        ActionPrune,
					Recording:   api.Recording{ID: id, Filename: filepath.Base(rs.FolderPath)},
					Reason:      ReasonDeletedOnServer,
					OldRelative: rs.FolderPath,
				})
			}
		}
	}

	return out, nil
}

func checkMassDeletionGuards(missing []string, serverIDs map[string]bool, state *State, opts ReconcileOptions) error {
	if opts.PruneEmpty {
		return nil
	}
	totalState := len(state.Recordings)
	if totalState == 0 {
		return nil
	}
	// Guard (a): empty server, non-empty archive.
	if len(serverIDs) == 0 {
		return fmt.Errorf("%w: server returned 0 recordings but local archive has %d (use --prune-empty to override)",
			ErrMassDeletion, totalState)
	}
	// Guard (b): >50% shrink in one run.
	if len(missing)*2 > totalState {
		return fmt.Errorf("%w: %d of %d recordings missing (>50%%, use --prune-empty to override)",
			ErrMassDeletion, len(missing), totalState)
	}
	return nil
}

func decideForRecording(rec api.Recording, state *State, fs Filesystem, opts ReconcileOptions) Action {
	expected := expectedRelativePath(rec)

	rs, known := state.Recordings[rec.ID]
	if !known {
		return Action{
			Kind:      ActionFetch,
			Recording: rec,
			Reason:    ReasonNew,
			Folder:    expected,
		}
	}

	// Rename detection: stored path differs from current expected path.
	if rs.FolderPath != "" && rs.FolderPath != expected {
		return Action{
			Kind:        ActionRename,
			Recording:   rec,
			Reason:      ReasonRename,
			OldRelative: rs.FolderPath,
			NewRelative: expected,
			Folder:      expected,
		}
	}

	// Layer B: server version differs from stored version.
	if opts.EnableVersionLayer && rs.Version != "" && rs.Version != rec.Version {
		return Action{
			Kind:      ActionFetch,
			Recording: rec,
			Reason:    ReasonVersionBump,
			Folder:    expected,
		}
	}

	// Folder presence + Layer A.
	meta, _ := fs.LoadMetadata(expected)
	if meta == nil {
		return Action{
			Kind:      ActionFetch,
			Recording: rec,
			Reason:    ReasonMissingLocally,
			Folder:    expected,
		}
	}

	// Layer A: server says an artifact is now ready, metadata.json says we
	// don't have it yet.
	if opts.Include.Transcript && rec.HasTranscript && meta.Transcript == nil {
		return Action{
			Kind:      ActionFetch,
			Recording: rec,
			Reason:    ReasonPresenceFlip,
			Folder:    expected,
		}
	}
	if opts.Include.Summary && rec.HasSummary && meta.Summary == nil {
		return Action{
			Kind:      ActionFetch,
			Recording: rec,
			Reason:    ReasonPresenceFlip,
			Folder:    expected,
		}
	}
	if opts.Include.Audio && meta.Audio == nil {
		return Action{
			Kind:      ActionFetch,
			Recording: rec,
			Reason:    ReasonPresenceFlip,
			Folder:    expected,
		}
	}

	return Action{
		Kind:      ActionSkip,
		Recording: rec,
		Reason:    ReasonVerified,
		Folder:    expected,
	}
}

// expectedRelativePath computes the slug-derived relative folder path for
// rec under spec 0002 F-03's layout. Returns slash-separated for stable
// state-file shape across platforms.
func expectedRelativePath(rec api.Recording) string {
	slug := archive.Slug(rec.Filename)
	if slug == "untitled" {
		slug = archive.SlugWithCollision(rec.Filename, rec.ID, func(s string) bool { return s == "untitled" })
	}
	ar := archive.Recording{
		ID:              rec.ID,
		TitleSlug:       slug,
		RecordedAtUTC:   rec.StartTime.UTC(),
		RecordedAtLocal: rec.StartTime,
	}
	folder, err := archive.RecordingFolder("", ar)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(folder)
}

// noopFS returns nil metadata for every lookup. Used as a default when the
// caller passes a nil Filesystem.
type noopFS struct{}

func (noopFS) LoadMetadata(string) (*archive.Metadata, error) { return nil, nil }
