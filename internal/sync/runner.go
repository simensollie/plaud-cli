package sync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/simensollie/plaud-cli/internal/api"
	"github.com/simensollie/plaud-cli/internal/archive"
	"github.com/simensollie/plaud-cli/internal/fetch"
)

// RunOptions configures one Run call. Mirrors the per-recording options
// fetch.FetchOne expects, plus the worker-pool size.
type RunOptions struct {
	Concurrency       int
	Include           archive.IncludeSet
	TranscriptFormats []string
	AudioFormat       string
	Force             bool
}

// RunResult is the aggregate outcome of one Run call (one cycle in
// watch mode, the whole run otherwise).
type RunResult struct {
	Discovered int
	Fetched    int
	Skipped    int
	Verified   int
	Failed     int
	Pruned     int
	// Status is "ok", "interrupted" (ctx cancelled), or "failed" (runner-
	// internal error; per-recording failures alone keep Status="ok").
	Status string
}

// EventKind enumerates the per-action notifications the Runner emits.
// Stable strings; Phase 6 maps to NDJSON event names.
type EventKind string

const (
	EventDiscovered EventKind = "discovered"
	EventSkipped    EventKind = "skipped"
	EventFetched    EventKind = "fetched"
	EventVerified   EventKind = "verified"
	EventFailed     EventKind = "failed"
	EventPruned     EventKind = "pruned"
)

// Event is one notification surfaced by the Runner. Phase 6 wraps these in
// the NDJSON envelope (F-07).
type Event struct {
	Kind   EventKind
	ID     string
	Reason Reason
	Action Action
	Result *fetch.Result
	Err    error
}

// Pruner is the Phase 4 hook for handling ActionPrune actions. The Runner
// dispatches prune actions through this interface so the prune package can
// stay independent of the runner.
type Pruner interface {
	Prune(ctx context.Context, root string, action Action) (PruneResult, error)
}

// PruneResult captures where the pruned folder ended up. Used by the
// Runner to update state and emit Pruned events.
type PruneResult struct {
	TrashPath string
}

// Runner drives a list of pre-reconciled Actions through the per-recording
// fetch primitive. Per-recording state writes (F-04) are serialized via an
// internal mutex; Save is atomic via tmp+rename.
type Runner struct {
	Client *api.Client
	Root   string
	State  *State
	Pruner Pruner
	Now    func() time.Time
	Emit   func(Event)
}

// Run executes actions with up to opts.Concurrency workers. Per-recording
// failures are captured in state.Recordings[id].LastError and surfaced as
// a Failed event; the run continues with other recordings (F-08).
//
// On context cancellation (SIGINT/SIGTERM in the cmd layer), in-flight
// workers drain to completion of their current HTTP request, then the
// Runner returns RunResult{Status: "interrupted"}.
func (r *Runner) Run(ctx context.Context, actions []Action, opts RunOptions) (RunResult, error) {
	if r.Now == nil {
		r.Now = func() time.Time { return time.Now().UTC() }
	}
	emit := r.Emit
	if emit == nil {
		emit = func(Event) {}
	}

	// Mutex protects state mutations and Save.
	var stateMu sync.Mutex

	// Persist the run-start checkpoint up front so even an immediate
	// cancel leaves a Last-Run-Started timestamp on disk (F-12 dry-run also
	// relies on this advancing).
	r.State.LastRunStarted = r.Now().UTC()
	if err := r.saveLocked(&stateMu); err != nil {
		return RunResult{Status: "failed"}, err
	}

	concurrency := opts.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}

	// emitDiscovered prelude lets consumers see the full set of recordings
	// the cycle is about to process (F-07).
	for _, a := range actions {
		emit(Event{Kind: EventDiscovered, ID: a.Recording.ID, Reason: a.Reason, Action: a})
	}

	type job struct {
		idx int
		act Action
	}
	jobs := make(chan job)
	var wg sync.WaitGroup
	results := make([]actionOutcome, len(actions))

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					results[j.idx] = actionOutcome{kind: outcomeInterrupted}
					emit(Event{Kind: EventFailed, ID: j.act.Recording.ID, Action: j.act, Err: ctx.Err()})
					continue
				}
				out := r.processAction(ctx, j.act, opts, &stateMu)
				results[j.idx] = out
				switch out.kind {
				case outcomeFetched:
					emit(Event{Kind: EventFetched, ID: j.act.Recording.ID, Reason: j.act.Reason, Action: j.act, Result: out.result})
				case outcomeSkipped:
					emit(Event{Kind: EventSkipped, ID: j.act.Recording.ID, Reason: j.act.Reason, Action: j.act, Result: out.result})
				case outcomeVerified:
					emit(Event{Kind: EventVerified, ID: j.act.Recording.ID, Reason: j.act.Reason, Action: j.act, Result: out.result})
				case outcomePruned:
					emit(Event{Kind: EventPruned, ID: j.act.Recording.ID, Action: j.act})
				case outcomeFailed:
					emit(Event{Kind: EventFailed, ID: j.act.Recording.ID, Action: j.act, Err: out.err})
				}
			}
		}()
	}

	for i, a := range actions {
		select {
		case <-ctx.Done():
			results[i] = actionOutcome{kind: outcomeInterrupted}
			emit(Event{Kind: EventFailed, ID: a.Recording.ID, Action: a, Err: ctx.Err()})
		case jobs <- job{idx: i, act: a}:
		}
	}
	close(jobs)
	wg.Wait()

	res := RunResult{Discovered: len(actions), Status: "ok"}
	for _, o := range results {
		switch o.kind {
		case outcomeFetched:
			res.Fetched++
		case outcomeSkipped:
			res.Skipped++
		case outcomeVerified:
			res.Verified++
		case outcomePruned:
			res.Pruned++
		case outcomeFailed:
			res.Failed++
		case outcomeInterrupted:
			res.Status = "interrupted"
		}
	}
	if ctx.Err() != nil {
		res.Status = "interrupted"
	}

	// Final state checkpoint: mark cycle finish.
	r.State.LastRunFinished = r.Now().UTC()
	if err := r.saveLocked(&stateMu); err != nil {
		return res, err
	}
	return res, nil
}

type outcomeKind int

const (
	outcomeFetched outcomeKind = iota + 1
	outcomeSkipped
	outcomeVerified
	outcomePruned
	outcomeFailed
	outcomeInterrupted
)

type actionOutcome struct {
	kind   outcomeKind
	result *fetch.Result
	err    error
}

// processAction handles one Action. Mutates state under stateMu and writes
// the state file before returning.
func (r *Runner) processAction(
	ctx context.Context,
	act Action,
	opts RunOptions,
	stateMu *sync.Mutex,
) actionOutcome {
	switch act.Kind {
	case ActionPrune:
		if r.Pruner == nil {
			// Phase 3 standalone: drop the prune; Phase 4 wires it in.
			return actionOutcome{kind: outcomeSkipped}
		}
		pr, err := r.Pruner.Prune(ctx, r.Root, act)
		if err != nil {
			r.recordFailure(act.Recording.ID, err, stateMu)
			return actionOutcome{kind: outcomeFailed, err: err}
		}
		stateMu.Lock()
		delete(r.State.Recordings, act.Recording.ID)
		stateMu.Unlock()
		_ = r.saveLocked(stateMu)
		_ = pr // for future use (event details)
		return actionOutcome{kind: outcomePruned}

	case ActionSkip:
		stateMu.Lock()
		rs := r.State.Recordings[act.Recording.ID]
		rs.Version = act.Recording.Version
		rs.VersionMs = act.Recording.VersionMs
		rs.FolderPath = act.Folder
		rs.LastError = nil
		r.State.Recordings[act.Recording.ID] = rs
		stateMu.Unlock()
		_ = r.saveLocked(stateMu)
		return actionOutcome{kind: outcomeSkipped}

	case ActionRename:
		newRel, err := r.performRename(act)
		if err != nil {
			r.recordFailure(act.Recording.ID, err, stateMu)
			return actionOutcome{kind: outcomeFailed, err: err}
		}
		// Update folder_path before fetching. FetchOne computes its own
		// folder from rec.Filename / rec.StartTime, which matches newRel by
		// construction; we just need state to know.
		stateMu.Lock()
		rs := r.State.Recordings[act.Recording.ID]
		rs.FolderPath = newRel
		r.State.Recordings[act.Recording.ID] = rs
		stateMu.Unlock()
		_ = r.saveLocked(stateMu)
		// Fall through to fetch.
		fallthrough

	case ActionFetch:
		fres := fetch.FetchOne(ctx, r.Client, r.Root, act.Recording, fetch.Options{
			Include:           opts.Include,
			TranscriptFormats: opts.TranscriptFormats,
			AudioFormat:       opts.AudioFormat,
			Force:             opts.Force,
			Slug:              slugFromState(act, r.State),
		}, r.Now)

		if fres.Err != nil {
			if errors.Is(fres.Err, context.Canceled) || errors.Is(fres.Err, context.DeadlineExceeded) {
				return actionOutcome{kind: outcomeInterrupted, err: fres.Err}
			}
			r.recordFailure(act.Recording.ID, fres.Err, stateMu)
			return actionOutcome{kind: outcomeFailed, err: fres.Err, result: &fres}
		}

		stateMu.Lock()
		rs := r.State.Recordings[act.Recording.ID]
		rs.Version = act.Recording.Version
		rs.VersionMs = act.Recording.VersionMs
		// FetchOne resolves the folder itself from filename + start_time;
		// store the relative version of fres.Folder as our pointer.
		rel, err := filepath.Rel(r.Root, fres.Folder)
		if err == nil {
			rs.FolderPath = filepath.ToSlash(rel)
		}
		rs.LastError = nil
		r.State.Recordings[act.Recording.ID] = rs
		stateMu.Unlock()
		_ = r.saveLocked(stateMu)

		switch fres.Status {
		case fetch.StatusSkipped:
			if opts.Force {
				return actionOutcome{kind: outcomeVerified, result: &fres}
			}
			return actionOutcome{kind: outcomeSkipped, result: &fres}
		case fetch.StatusFetched:
			if opts.Force {
				return actionOutcome{kind: outcomeVerified, result: &fres}
			}
			return actionOutcome{kind: outcomeFetched, result: &fres}
		default:
			return actionOutcome{kind: outcomeFailed, err: fmt.Errorf("unexpected fetch status %q", fres.Status), result: &fres}
		}
	}
	return actionOutcome{kind: outcomeSkipped}
}

// performRename moves the recording's folder from old to new, applying
// the F-03 6-char-id suffix on collision (F-16). Returns the actual new
// relative path used (may differ from act.NewRelative when collision-
// resolved). Empty parent directories under YYYY/MM are not auto-removed.
func (r *Runner) performRename(act Action) (string, error) {
	oldAbs := filepath.Join(r.Root, filepath.FromSlash(act.OldRelative))
	target := act.NewRelative

	// If target exists with a different recording's metadata, resolve
	// collision via F-03 id suffix.
	collisionAbs := filepath.Join(r.Root, filepath.FromSlash(target))
	if otherRec, conflicts := folderConflicts(collisionAbs, act.Recording.ID); conflicts {
		_ = otherRec // could log
		base := filepath.Base(target)
		dir := filepath.Dir(target)
		suffix := act.Recording.ID
		if len(suffix) > 6 {
			suffix = suffix[:6]
		}
		target = filepath.Join(dir, base+"_"+suffix)
		target = filepath.ToSlash(target)
	}

	newAbs := filepath.Join(r.Root, filepath.FromSlash(target))
	if err := os.MkdirAll(filepath.Dir(newAbs), 0o755); err != nil {
		return "", fmt.Errorf("creating target parent: %w", err)
	}
	// If the source exists, move it. If it doesn't, the next fetch creates
	// the target fresh (the no-metadata-walk-repair stance from Q10).
	if _, err := os.Stat(oldAbs); err == nil {
		if err := os.Rename(oldAbs, newAbs); err != nil {
			return "", fmt.Errorf("renaming %s -> %s: %w", oldAbs, newAbs, err)
		}
	}
	return target, nil
}

// slugFromState extracts the slug from the action's resolved folder path
// (or from state, if the runner just rewrote state.FolderPath after a
// rename). The leaf format is YYYY-MM-DD_HHMM_<slug>.
func slugFromState(act Action, state *State) string {
	rs, ok := state.Recordings[act.Recording.ID]
	rel := act.Folder
	if ok && rs.FolderPath != "" {
		rel = rs.FolderPath
	}
	if rel == "" {
		return ""
	}
	base := filepath.Base(rel)
	parts := strings.SplitN(base, "_", 3)
	if len(parts) < 3 {
		return ""
	}
	return parts[2]
}

// folderConflicts reports whether path holds a metadata.json belonging to a
// different recording. Returns the conflicting id when true; empty string
// when the folder is missing, has no metadata, or matches our id.
func folderConflicts(path, ourID string) (string, bool) {
	mp := filepath.Join(path, archive.MetadataFilename)
	raw, err := os.ReadFile(mp)
	if err != nil {
		return "", false
	}
	m, err := archive.UnmarshalMetadata(raw)
	if err != nil || m == nil {
		return "", false
	}
	if m.ID == "" || m.ID == ourID {
		return "", false
	}
	return m.ID, true
}

// recordFailure records the per-recording error in state and saves.
func (r *Runner) recordFailure(id string, err error, stateMu *sync.Mutex) {
	stateMu.Lock()
	rs := r.State.Recordings[id]
	rs.LastError = &RecordingError{Msg: api.RedactError(err), At: r.Now().UTC()}
	r.State.Recordings[id] = rs
	stateMu.Unlock()
	_ = r.saveLocked(stateMu)
}

// saveLocked acquires stateMu, then writes the state file. Internal helper
// so processAction's per-segment locking remains tight.
func (r *Runner) saveLocked(stateMu *sync.Mutex) error {
	stateMu.Lock()
	defer stateMu.Unlock()
	return Save(r.Root, r.State)
}
