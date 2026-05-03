package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestState_F03_RoundTrip(t *testing.T) {
	root := t.TempDir()
	in := &State{
		SchemaVersion:   1,
		LastRunStarted:  time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		LastRunFinished: time.Date(2026, 5, 1, 12, 0, 42, 0, time.UTC),
		Recordings: map[string]RecordingState{
			"a3f9c021000000000000000000000001": {
				Version:    "v17",
				VersionMs:  1735689600000,
				FolderPath: "2026/04/2026-04-30_1430_kickoff",
			},
			"b7e2d018000000000000000000000002": {
				Version:    "v3",
				VersionMs:  1735689700000,
				FolderPath: "2026/04/2026-04-30_1500_standup",
				LastError: &RecordingError{
					Msg: "transient failure",
					At:  time.Date(2026, 5, 1, 11, 59, 30, 0, time.UTC),
				},
			},
		},
	}
	if err := Save(root, in); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.SchemaVersion != 1 {
		t.Errorf("schema_version=%d, want 1", got.SchemaVersion)
	}
	if !got.LastRunStarted.Equal(in.LastRunStarted) {
		t.Errorf("last_run_started=%v, want %v", got.LastRunStarted, in.LastRunStarted)
	}
	if len(got.Recordings) != 2 {
		t.Errorf("got %d recordings, want 2", len(got.Recordings))
	}
	if r := got.Recordings["a3f9c021000000000000000000000001"]; r.FolderPath != "2026/04/2026-04-30_1430_kickoff" {
		t.Errorf("folder_path=%q", r.FolderPath)
	}
	if r := got.Recordings["b7e2d018000000000000000000000002"]; r.LastError == nil || r.LastError.Msg != "transient failure" {
		t.Errorf("last_error round-trip lost: %+v", r.LastError)
	}
}

func TestState_F03_FolderPathIsRelativeToArchiveRoot(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, "2026", "04", "2026-04-30_1430_kickoff")
	in := &State{
		SchemaVersion: 1,
		Recordings: map[string]RecordingState{
			"id1": {FolderPath: abs},
		},
	}
	if err := Save(root, in); err == nil {
		t.Errorf("expected error storing absolute folder_path, got nil")
	} else if !strings.Contains(err.Error(), "absolute") && !strings.Contains(err.Error(), "relative") {
		t.Errorf("error %q does not mention absolute/relative", err)
	}
}

func TestState_F04_AtomicWriteSurvivesCrashMidWrite(t *testing.T) {
	root := t.TempDir()
	good := &State{
		SchemaVersion:   1,
		LastRunStarted:  time.Now().UTC(),
		LastRunFinished: time.Now().UTC(),
		Recordings: map[string]RecordingState{
			"id1": {Version: "v1", FolderPath: "2026/04/folder"},
		},
	}
	if err := Save(root, good); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	// Simulate a crash mid-write: leave a corrupt .tmp file alongside the
	// real one. Load must ignore the .tmp and return the real state.
	tmp := filepath.Join(root, stateFilename+".tmp")
	if err := os.WriteFile(tmp, []byte("{this-is-not-json"), 0o644); err != nil {
		t.Fatalf("seeding tmp: %v", err)
	}

	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load after crash sim: %v", err)
	}
	if got.Recordings["id1"].Version != "v1" {
		t.Errorf("crash sim corrupted load: %+v", got)
	}
}

func TestState_F03_MissingStateFileTreatedAsFreshIndex(t *testing.T) {
	root := t.TempDir()
	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load on empty root: %v", err)
	}
	if got == nil {
		t.Fatalf("Load returned nil state")
	}
	if got.SchemaVersion != 1 {
		t.Errorf("fresh state schema_version=%d, want 1", got.SchemaVersion)
	}
	if len(got.Recordings) != 0 {
		t.Errorf("fresh state should have no recordings, got %d", len(got.Recordings))
	}
}

func TestState_F13_LastErrorRedactedOnSave(t *testing.T) {
	root := t.TempDir()
	in := &State{
		SchemaVersion: 1,
		Recordings: map[string]RecordingState{
			"id1": {
				FolderPath: "2026/04/folder",
				LastError: &RecordingError{
					Msg: "GET https://euc1-prod-plaud-bucket.s3.amazonaws.com/x?X-Amz-Signature=deadbeef returned 403 with Bearer eyJabc.def.ghi",
					At:  time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
				},
			},
		},
	}
	if err := Save(root, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, stateFilename))
	if err != nil {
		t.Fatalf("reading state file: %v", err)
	}
	for _, leak := range []string{"deadbeef", "eyJabc.def.ghi", "X-Amz-Signature=deadbeef", "amazonaws.com"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("F-13: leak %q in state file:\n%s", leak, raw)
		}
	}
}

// Sanity: the state file is JSON the tool can deserialize. Catches schema
// drift between Save() and Load().
func TestState_F03_OnDiskShapeMatchesSpec(t *testing.T) {
	root := t.TempDir()
	in := &State{
		SchemaVersion:   1,
		LastRunStarted:  time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		LastRunFinished: time.Date(2026, 5, 1, 12, 0, 42, 0, time.UTC),
		Recordings: map[string]RecordingState{
			"id1": {Version: "v1", VersionMs: 12345, FolderPath: "2026/04/folder"},
		},
	}
	if err := Save(root, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, stateFilename))
	if err != nil {
		t.Fatalf("reading state file: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("state JSON malformed: %v\n%s", err, raw)
	}
	for _, key := range []string{"schema_version", "last_run_started", "last_run_finished", "recordings"} {
		if _, ok := generic[key]; !ok {
			t.Errorf("state missing %q at top level: %s", key, raw)
		}
	}
	recs, _ := generic["recordings"].(map[string]any)
	if recs == nil {
		t.Fatal("recordings is not an object")
	}
	r1, _ := recs["id1"].(map[string]any)
	if r1 == nil {
		t.Fatal("id1 missing")
	}
	for _, key := range []string{"version", "version_ms", "folder_path"} {
		if _, ok := r1[key]; !ok {
			t.Errorf("id1 missing %q: %+v", key, r1)
		}
	}
}
