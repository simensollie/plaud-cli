package sync

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/simensollie/plaud-cli/internal/api"
	"github.com/simensollie/plaud-cli/internal/archive"
)

// fakeFS is an in-memory archive.Metadata lookup keyed by relative folder
// path; nil entries mean "absent" (no metadata.json on disk).
type fakeFS struct {
	by map[string]*archive.Metadata
}

func (f *fakeFS) LoadMetadata(rel string) (*archive.Metadata, error) {
	if f == nil || f.by == nil {
		return nil, nil
	}
	m, ok := f.by[rel]
	if !ok {
		return nil, nil
	}
	return m, nil
}

func mkRec(id, filename string, version string, versionMs int64, hasTrans, hasSummary bool) api.Recording {
	return api.Recording{
		ID:            id,
		Filename:      filename,
		StartTime:     time.Date(2026, 4, 30, 14, 30, 0, 0, time.UTC),
		Duration:      time.Hour,
		HasTranscript: hasTrans,
		HasSummary:    hasSummary,
		Version:       version,
		VersionMs:     versionMs,
	}
}

func mkMeta(transcript, summary, audio bool) *archive.Metadata {
	m := &archive.Metadata{
		ArchiveSchemaVersion: 1,
		ClientVersion:        "test",
		ID:                   "any",
	}
	if transcript {
		m.Transcript = &archive.MetaTranscript{Filename: "transcript.json", SHA256: "deadbeef"}
	}
	if summary {
		m.Summary = &archive.MetaSummary{Filename: "summary.plaud.md", SHA256: "deadbeef"}
	}
	if audio {
		m.Audio = &archive.MetaAudio{Filename: "audio.mp3", S3ETag: "deadbeef"}
	}
	return m
}

func defaultIncludeTextOnly() archive.IncludeSet {
	return archive.IncludeSet{Transcript: true, Summary: true, Metadata: true}
}

func TestReconcile_F01_NewRecordingsScheduledForFetch(t *testing.T) {
	rec := mkRec("a3f9c021000000000000000000000001", "Kickoff", "v1", 100, true, true)
	state := freshState()
	got, err := Reconcile([]api.Recording{rec}, nil, state, &fakeFS{}, ReconcileOptions{Include: defaultIncludeTextOnly()})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(got.Actions) != 1 {
		t.Fatalf("got %d actions, want 1", len(got.Actions))
	}
	if got.Actions[0].Kind != ActionFetch {
		t.Errorf("kind=%q, want fetch", got.Actions[0].Kind)
	}
	if got.Actions[0].Reason != ReasonNew {
		t.Errorf("reason=%q, want new", got.Actions[0].Reason)
	}
}

func TestReconcile_F02_VerifiedRecordingsSkipped(t *testing.T) {
	rec := mkRec("id1", "Kickoff", "v1", 100, true, true)
	folder := "2026/04/2026-04-30_1430_kickoff"
	state := &State{
		SchemaVersion: 1,
		Recordings: map[string]RecordingState{
			rec.ID: {Version: "v1", VersionMs: 100, FolderPath: folder},
		},
	}
	fs := &fakeFS{by: map[string]*archive.Metadata{folder: mkMeta(true, true, false)}}
	got, err := Reconcile([]api.Recording{rec}, nil, state, fs, ReconcileOptions{Include: defaultIncludeTextOnly()})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(got.Actions) != 1 || got.Actions[0].Kind != ActionSkip {
		t.Fatalf("expected one skip, got %+v", got.Actions)
	}
}

func TestReconcile_F09_ServerDeletedSchedulesPrune_OnlyWithFlag(t *testing.T) {
	state := &State{
		SchemaVersion: 1,
		Recordings: map[string]RecordingState{
			"id1": {Version: "v1", FolderPath: "2026/04/folder1"},
			"id2": {Version: "v1", FolderPath: "2026/04/folder2"},
		},
	}
	fs := &fakeFS{by: map[string]*archive.Metadata{
		"2026/04/folder1": mkMeta(true, true, false),
		"2026/04/folder2": mkMeta(true, true, false),
	}}

	rec1 := mkRec("id1", "Folder1", "v1", 100, true, true)

	// Without --prune: id2 missing from server → no prune action.
	got, err := Reconcile([]api.Recording{rec1}, nil, state, fs, ReconcileOptions{Include: defaultIncludeTextOnly()})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	for _, a := range got.Actions {
		if a.Kind == ActionPrune {
			t.Errorf("unexpected prune without --prune: %+v", a)
		}
	}

	// With --prune: id2 scheduled for prune.
	got, err = Reconcile([]api.Recording{rec1}, nil, state, fs, ReconcileOptions{Include: defaultIncludeTextOnly(), Prune: true})
	if err != nil {
		t.Fatalf("Reconcile (prune): %v", err)
	}
	pruneCount := 0
	for _, a := range got.Actions {
		if a.Kind == ActionPrune && a.Recording.ID == "id2" {
			pruneCount++
		}
	}
	if pruneCount != 1 {
		t.Fatalf("expected 1 prune action for id2, got %d (%+v)", pruneCount, got.Actions)
	}
}

func TestReconcile_F09_UnionOfTrashFiltersExcludesWebUITrashed(t *testing.T) {
	state := &State{
		SchemaVersion: 1,
		Recordings: map[string]RecordingState{
			"id1": {Version: "v1", FolderPath: "2026/04/folder1"},
			"id2": {Version: "v1", FolderPath: "2026/04/folder2"},
		},
	}
	fs := &fakeFS{by: map[string]*archive.Metadata{
		"2026/04/folder1": mkMeta(true, true, false),
		"2026/04/folder2": mkMeta(true, true, false),
	}}
	rec1 := mkRec("id1", "Folder1", "v1", 100, true, true)
	// id2 is in the trashed list (web-UI trashed). It's missing from is_trash=0
	// but present in is_trash=1; with --prune, must NOT be pruned.
	rec2Trashed := mkRec("id2", "Folder2", "v1", 100, true, true)
	rec2Trashed.IsTrash = true

	got, err := Reconcile([]api.Recording{rec1}, []api.Recording{rec2Trashed}, state, fs, ReconcileOptions{
		Include: defaultIncludeTextOnly(),
		Prune:   true,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	for _, a := range got.Actions {
		if a.Kind == ActionPrune && a.Recording.ID == "id2" {
			t.Fatalf("F-09: id2 (web-UI-trashed) was pruned: %+v", a)
		}
	}
}

func TestReconcile_F09_MassDeletionGuardRefusesWithoutPruneEmpty(t *testing.T) {
	state := &State{
		SchemaVersion: 1,
		Recordings: map[string]RecordingState{
			"id1": {Version: "v1", FolderPath: "f1"},
			"id2": {Version: "v1", FolderPath: "f2"},
			"id3": {Version: "v1", FolderPath: "f3"},
			"id4": {Version: "v1", FolderPath: "f4"},
		},
	}
	// Server returns nothing in either list.
	_, err := Reconcile(nil, nil, state, &fakeFS{}, ReconcileOptions{
		Include: defaultIncludeTextOnly(),
		Prune:   true,
	})
	if err == nil {
		t.Fatalf("F-09: empty-server with non-empty archive should refuse without --prune-empty")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "prune-empty") {
		t.Errorf("error should mention --prune-empty, got %q", err)
	}

	// 50% threshold: archive has 4, server returns 1 → prune 3 (75%) → refuse.
	rec1 := mkRec("id1", "f1", "v1", 100, true, true)
	_, err = Reconcile([]api.Recording{rec1}, nil, state, &fakeFS{}, ReconcileOptions{
		Include: defaultIncludeTextOnly(),
		Prune:   true,
	})
	if err == nil {
		t.Fatalf("F-09: >50%% prune should refuse without --prune-empty")
	}

	// With --prune-empty, both pass.
	_, err = Reconcile(nil, nil, state, &fakeFS{}, ReconcileOptions{
		Include:    defaultIncludeTextOnly(),
		Prune:      true,
		PruneEmpty: true,
	})
	if err != nil {
		t.Fatalf("F-09: --prune-empty should bypass the empty-server guard, got %v", err)
	}
}

func TestReconcile_F10_TrashedHonorsIncludeTrashedFlag(t *testing.T) {
	rec := mkRec("id1", "Trashed", "v1", 100, true, true)
	rec.IsTrash = true

	// Default: trashed recordings (only present in listTrashed) are not fetched.
	got, err := Reconcile(nil, []api.Recording{rec}, freshState(), &fakeFS{}, ReconcileOptions{Include: defaultIncludeTextOnly()})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	for _, a := range got.Actions {
		if a.Kind == ActionFetch {
			t.Errorf("default mode scheduled trashed for fetch: %+v", a)
		}
	}

	// --include-trashed: scheduled.
	got, err = Reconcile(nil, []api.Recording{rec}, freshState(), &fakeFS{}, ReconcileOptions{
		Include:        defaultIncludeTextOnly(),
		IncludeTrashed: true,
	})
	if err != nil {
		t.Fatalf("Reconcile (--include-trashed): %v", err)
	}
	fetched := 0
	for _, a := range got.Actions {
		if a.Kind == ActionFetch && a.Recording.ID == "id1" {
			fetched++
		}
	}
	if fetched != 1 {
		t.Fatalf("expected 1 fetch for trashed under --include-trashed, got %d", fetched)
	}
}

func TestReconcile_F14_DefaultIncludeIsTextOnly(t *testing.T) {
	// Sanity: the helper used by tests reflects the spec default.
	def := defaultIncludeTextOnly()
	if def.Audio {
		t.Errorf("F-14: default include should not include audio")
	}
	if !def.Transcript || !def.Summary || !def.Metadata {
		t.Errorf("F-14: default include should include transcript, summary, metadata; got %+v", def)
	}
}

func TestReconcile_F15_LayerA_PresenceFlipReFetchesNewlyAvailableArtifact(t *testing.T) {
	// State knows the recording at v1; metadata.json has summary but no
	// transcript (transcript wasn't ready last run). Server now reports
	// is_trans=true. Reconciler must schedule fetch for the transcript.
	rec := mkRec("id1", "Kickoff", "v1", 100, true, true)
	folder := "2026/04/2026-04-30_1430_kickoff"
	state := &State{
		SchemaVersion: 1,
		Recordings: map[string]RecordingState{
			"id1": {Version: "v1", VersionMs: 100, FolderPath: folder},
		},
	}
	fs := &fakeFS{by: map[string]*archive.Metadata{folder: mkMeta(false, true, false)}}

	got, err := Reconcile([]api.Recording{rec}, nil, state, fs, ReconcileOptions{Include: defaultIncludeTextOnly()})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(got.Actions) != 1 || got.Actions[0].Kind != ActionFetch {
		t.Fatalf("expected one fetch action, got %+v", got.Actions)
	}
	if got.Actions[0].Reason != ReasonPresenceFlip {
		t.Errorf("reason=%q, want presence-flip", got.Actions[0].Reason)
	}
}

func TestReconcile_F15_LayerB_VersionBumpReFetches(t *testing.T) {
	// Gated on Phase 0 Probe A: only enabled when EnableVersionLayer=true.
	rec := mkRec("id1", "Kickoff", "v2", 200, true, true)
	folder := "2026/04/2026-04-30_1430_kickoff"
	state := &State{
		SchemaVersion: 1,
		Recordings: map[string]RecordingState{
			"id1": {Version: "v1", VersionMs: 100, FolderPath: folder},
		},
	}
	fs := &fakeFS{by: map[string]*archive.Metadata{folder: mkMeta(true, true, false)}}

	// Layer B disabled (default): we'd normally skip because state version
	// differs but that's not Layer A territory. With Layer B off, drift is
	// invisible.
	got, err := Reconcile([]api.Recording{rec}, nil, state, fs, ReconcileOptions{Include: defaultIncludeTextOnly()})
	if err != nil {
		t.Fatalf("Reconcile (layer B off): %v", err)
	}
	for _, a := range got.Actions {
		if a.Kind == ActionFetch && a.Reason == ReasonVersionBump {
			t.Errorf("Layer B should be off by default, got %+v", a)
		}
	}

	// Layer B enabled: version mismatch triggers fetch.
	got, err = Reconcile([]api.Recording{rec}, nil, state, fs, ReconcileOptions{
		Include:            defaultIncludeTextOnly(),
		EnableVersionLayer: true,
	})
	if err != nil {
		t.Fatalf("Reconcile (layer B on): %v", err)
	}
	versionBumpCount := 0
	for _, a := range got.Actions {
		if a.Kind == ActionFetch && a.Reason == ReasonVersionBump {
			versionBumpCount++
		}
	}
	if versionBumpCount != 1 {
		t.Fatalf("F-15 Layer B: expected 1 version-bump fetch, got %d (%+v)", versionBumpCount, got.Actions)
	}
}

func TestReconcile_F16_RenameDetectedViaIDAndRelativePath(t *testing.T) {
	// Recording id1 was at slug "kickoff"; server now reports filename
	// "Standup". Reconciler must emit a Rename action with the old and new
	// relative paths.
	rec := mkRec("id1", "Standup", "v1", 100, true, true)
	oldPath := "2026/04/2026-04-30_1430_kickoff"
	state := &State{
		SchemaVersion: 1,
		Recordings: map[string]RecordingState{
			"id1": {Version: "v1", VersionMs: 100, FolderPath: oldPath},
		},
	}
	fs := &fakeFS{by: map[string]*archive.Metadata{oldPath: mkMeta(true, true, false)}}

	got, err := Reconcile([]api.Recording{rec}, nil, state, fs, ReconcileOptions{Include: defaultIncludeTextOnly()})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	var rename *Action
	for i := range got.Actions {
		if got.Actions[i].Kind == ActionRename {
			rename = &got.Actions[i]
			break
		}
	}
	if rename == nil {
		t.Fatalf("F-16: expected rename action, got %+v", got.Actions)
	}
	if rename.OldRelative != oldPath {
		t.Errorf("OldRelative=%q, want %q", rename.OldRelative, oldPath)
	}
	if rename.NewRelative == "" || rename.NewRelative == oldPath {
		t.Errorf("NewRelative=%q, want different from old", rename.NewRelative)
	}
	if !strings.Contains(rename.NewRelative, "standup") {
		t.Errorf("NewRelative=%q should reflect new slug", rename.NewRelative)
	}
}

// Ensures the reconciler doesn't crash on a missing server list when there's
// also no state. The first-run path.
func TestReconcile_FirstRunNoState(t *testing.T) {
	got, err := Reconcile(nil, nil, freshState(), &fakeFS{}, ReconcileOptions{Include: defaultIncludeTextOnly()})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(got.Actions) != 0 {
		t.Errorf("expected 0 actions on empty inputs, got %+v", got.Actions)
	}
}

// Sanity: a sentinel mass-deletion error wraps a typed error so callers
// can detect it via errors.Is.
func TestReconcile_F09_MassDeletionErrorIsTyped(t *testing.T) {
	state := &State{
		SchemaVersion: 1,
		Recordings: map[string]RecordingState{
			"id1": {FolderPath: "f1"},
		},
	}
	_, err := Reconcile(nil, nil, state, &fakeFS{}, ReconcileOptions{
		Include: defaultIncludeTextOnly(),
		Prune:   true,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrMassDeletion) {
		t.Errorf("error should wrap ErrMassDeletion: %v", err)
	}
}
