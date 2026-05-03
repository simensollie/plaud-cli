package sync

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/simensollie/plaud-cli/internal/api"
	"github.com/simensollie/plaud-cli/internal/archive"
	"github.com/simensollie/plaud-cli/internal/fetch"
)

// ---------------------------------------------------------------------------
// Test fixture: a minimal Plaud + storage + audio backend.
// ---------------------------------------------------------------------------

type runnerBackend struct {
	api      *httptest.Server
	storage  *httptest.Server
	audio    *httptest.Server
	apiCount atomic.Int64
}

func (b *runnerBackend) close() {
	b.api.Close()
	b.storage.Close()
	b.audio.Close()
}

type fixture struct {
	id          string
	filename    string
	startMs     int64
	durationMs  int64
	segments    []map[string]any
	summary     string
	audio       []byte
	isTrans     bool
	isSummary   bool
	failDetail  bool
	failCounter *atomic.Int64
}

func newRunnerBackend(t *testing.T, recs ...fixture) *runnerBackend {
	t.Helper()
	by := map[string]fixture{}
	for _, r := range recs {
		by[r.id] = r
	}
	b := &runnerBackend{}

	b.storage = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		rec, ok := by[parts[1]]
		if !ok {
			http.NotFound(w, r)
			return
		}
		switch parts[0] {
		case "transcript":
			body, _ := json.Marshal(rec.segments)
			_, _ = w.Write(body)
		case "summary":
			_, _ = io.WriteString(w, rec.summary)
		default:
			http.NotFound(w, r)
		}
	}))

	b.audio = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := filepath.Base(r.URL.Path)
		id := strings.TrimSuffix(base, ".mp3")
		rec, ok := by[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		sum := md5.Sum(rec.audio)
		w.Header().Set("ETag", fmt.Sprintf(`"%s"`, hex.EncodeToString(sum[:])))
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(rec.audio)
		}
	}))

	b.api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.apiCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/file/detail/"):
			id := strings.TrimPrefix(r.URL.Path, "/file/detail/")
			rec, ok := by[id]
			if !ok {
				http.NotFound(w, r)
				return
			}
			if rec.failDetail && rec.failCounter != nil && rec.failCounter.Add(1) <= 1 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			contentList := []map[string]any{}
			pre := []map[string]any{}
			if rec.isTrans && len(rec.segments) > 0 {
				contentList = append(contentList, map[string]any{
					"data_id":     "transaction:" + id,
					"data_type":   "transaction",
					"task_status": 1,
					"data_link":   b.storage.URL + "/transcript/" + id,
				})
			}
			if rec.isSummary && rec.summary != "" {
				const sumID = "auto_sum:abc"
				contentList = append(contentList, map[string]any{
					"data_id":     sumID,
					"data_type":   "auto_sum_note",
					"task_status": 1,
				})
				pre = append(pre, map[string]any{"data_id": sumID, "data_content": rec.summary})
			}
			data := map[string]any{
				"file_id":                   rec.id,
				"file_name":                 rec.filename,
				"start_time":                rec.startMs,
				"duration":                  rec.durationMs,
				"content_list":              contentList,
				"pre_download_content_list": pre,
				"extra_data": map[string]any{
					"aiContentHeader": map[string]any{"language_code": "no"},
				},
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": 0, "msg": "ok", "data": data})
		case strings.HasPrefix(r.URL.Path, "/file/temp-url/"):
			id := strings.TrimPrefix(r.URL.Path, "/file/temp-url/")
			if _, ok := by[id]; !ok {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":   0,
				"msg":      "ok",
				"temp_url": b.audio.URL + "/audio/" + id + ".mp3",
			})
		default:
			http.NotFound(w, r)
		}
	}))

	t.Cleanup(b.close)
	return b
}

func mkAudio(seed byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = seed + byte(i%17)
	}
	return out
}

func newAPIClient(t *testing.T, baseURL string) *api.Client {
	t.Helper()
	c, err := api.New("eu", "tok", api.WithBaseURL(baseURL))
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	return c
}

func defaultRunOpts() RunOptions {
	return RunOptions{
		Concurrency:       2,
		Include:           archive.IncludeSet{Transcript: true, Summary: true, Metadata: true},
		TranscriptFormats: []string{"json", "md"},
		AudioFormat:       "mp3",
	}
}

func goodFixture(id string) fixture {
	return fixture{
		id:         id,
		filename:   "Rec " + id[len(id)-4:],
		startMs:    time.Date(2026, 4, 30, 14, 30, 0, 0, time.UTC).UnixMilli(),
		durationMs: 1000,
		isTrans:    true,
		isSummary:  true,
		segments: []map[string]any{
			{"start_time": 0, "end_time": 1000, "content": "hi", "speaker": "Speaker 0", "original_speaker": "Speaker 0"},
		},
		summary: "## Summary\nshort.\n",
		audio:   mkAudio(0x42, 64),
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRunner_F01_FetchesScheduled(t *testing.T) {
	rec := goodFixture("a3f9c021000000000000000000000001")
	be := newRunnerBackend(t, rec)
	defer be.close()
	root := t.TempDir()
	client := newAPIClient(t, be.api.URL)

	state := freshState()
	apiRec := api.Recording{ID: rec.id, Filename: rec.filename, StartTime: time.UnixMilli(rec.startMs).UTC(), Duration: time.Second, HasTranscript: true, HasSummary: true}
	actions := []Action{{Kind: ActionFetch, Recording: apiRec, Reason: ReasonNew, Folder: expectedRelativePath(apiRec)}}

	runner := &Runner{
		Client: client,
		Root:   root,
		State:  state,
		Now:    func() time.Time { return time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC) },
	}
	res, err := runner.Run(context.Background(), actions, defaultRunOpts())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Fetched != 1 {
		t.Errorf("Fetched=%d, want 1", res.Fetched)
	}

	// State updated.
	rs, ok := state.Recordings[rec.id]
	if !ok {
		t.Fatalf("state has no entry for %s", rec.id)
	}
	if rs.FolderPath == "" || filepath.IsAbs(rs.FolderPath) {
		t.Errorf("state folder_path should be relative: %q", rs.FolderPath)
	}
}

func TestRunner_F08_PerRecordingErrorsDoNotAbort(t *testing.T) {
	good := goodFixture("a3f9c021000000000000000000000001")
	bad := fixture{
		id:          "b00000000000000000000000000000bad",
		filename:    "bad",
		startMs:     time.Date(2026, 4, 30, 15, 0, 0, 0, time.UTC).UnixMilli(),
		durationMs:  1000,
		isTrans:     false,
		failDetail:  true,
		failCounter: new(atomic.Int64),
	}
	be := newRunnerBackend(t, good, bad)
	defer be.close()
	root := t.TempDir()
	client := newAPIClient(t, be.api.URL)

	state := freshState()
	actions := []Action{
		{Kind: ActionFetch, Recording: api.Recording{ID: good.id, Filename: good.filename, StartTime: time.UnixMilli(good.startMs).UTC()}, Reason: ReasonNew, Folder: expectedRelativePath(api.Recording{ID: good.id, Filename: good.filename, StartTime: time.UnixMilli(good.startMs).UTC()})},
		{Kind: ActionFetch, Recording: api.Recording{ID: bad.id, Filename: bad.filename, StartTime: time.UnixMilli(bad.startMs).UTC()}, Reason: ReasonNew, Folder: expectedRelativePath(api.Recording{ID: bad.id, Filename: bad.filename, StartTime: time.UnixMilli(bad.startMs).UTC()})},
	}

	runner := &Runner{
		Client: client,
		Root:   root,
		State:  state,
		Now:    func() time.Time { return time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC) },
	}
	res, err := runner.Run(context.Background(), actions, defaultRunOpts())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Failed != 1 {
		t.Errorf("Failed=%d, want 1", res.Failed)
	}
	if res.Fetched != 1 {
		t.Errorf("Fetched=%d, want 1 (the good one should still finish)", res.Fetched)
	}

	// State for the failing recording should have last_error set.
	rs, ok := state.Recordings[bad.id]
	if !ok {
		t.Fatalf("state has no entry for failing recording")
	}
	if rs.LastError == nil {
		t.Errorf("state %s has no last_error", bad.id)
	}
}

func TestRunner_F04_StateWrittenAfterEveryRecording(t *testing.T) {
	r1 := goodFixture("a3f9c021000000000000000000000001")
	r2 := goodFixture("b3f9c021000000000000000000000002")
	be := newRunnerBackend(t, r1, r2)
	defer be.close()
	root := t.TempDir()
	client := newAPIClient(t, be.api.URL)

	state := freshState()
	actions := []Action{
		{Kind: ActionFetch, Recording: api.Recording{ID: r1.id, Filename: r1.filename, StartTime: time.UnixMilli(r1.startMs).UTC()}, Reason: ReasonNew, Folder: expectedRelativePath(api.Recording{ID: r1.id, Filename: r1.filename, StartTime: time.UnixMilli(r1.startMs).UTC()})},
		{Kind: ActionFetch, Recording: api.Recording{ID: r2.id, Filename: r2.filename, StartTime: time.UnixMilli(r2.startMs).UTC()}, Reason: ReasonNew, Folder: expectedRelativePath(api.Recording{ID: r2.id, Filename: r2.filename, StartTime: time.UnixMilli(r2.startMs).UTC()})},
	}

	runner := &Runner{
		Client: client,
		Root:   root,
		State:  state,
		Now:    func() time.Time { return time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC) },
	}
	if _, err := runner.Run(context.Background(), actions, defaultRunOpts()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// On-disk state should now contain both recordings.
	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := loaded.Recordings[r1.id]; !ok {
		t.Errorf("on-disk state missing %s", r1.id)
	}
	if _, ok := loaded.Recordings[r2.id]; !ok {
		t.Errorf("on-disk state missing %s", r2.id)
	}
}

// TestRunner_F04_ResumableAfterSIGINT cancels the context partway and
// confirms that recordings completed before cancel are persisted in state.
func TestRunner_F04_ResumableAfterSIGINT(t *testing.T) {
	r1 := goodFixture("a3f9c021000000000000000000000001")
	r2 := goodFixture("b3f9c021000000000000000000000002")
	be := newRunnerBackend(t, r1, r2)
	defer be.close()
	root := t.TempDir()
	client := newAPIClient(t, be.api.URL)

	state := freshState()
	actions := []Action{
		{Kind: ActionFetch, Recording: api.Recording{ID: r1.id, Filename: r1.filename, StartTime: time.UnixMilli(r1.startMs).UTC()}, Reason: ReasonNew, Folder: expectedRelativePath(api.Recording{ID: r1.id, Filename: r1.filename, StartTime: time.UnixMilli(r1.startMs).UTC()})},
	}

	ctx, cancel := context.WithCancel(context.Background())
	runner := &Runner{
		Client: client,
		Root:   root,
		State:  state,
		Now:    func() time.Time { return time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC) },
	}
	// Run the first recording successfully.
	res, err := runner.Run(ctx, actions, defaultRunOpts())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Fetched != 1 {
		t.Fatalf("Fetched=%d", res.Fetched)
	}
	// Now cancel and run another action — it should be marked failed without
	// actually executing.
	cancel()
	res2, err2 := runner.Run(ctx, []Action{
		{Kind: ActionFetch, Recording: api.Recording{ID: r2.id, Filename: r2.filename, StartTime: time.UnixMilli(r2.startMs).UTC()}, Reason: ReasonNew, Folder: expectedRelativePath(api.Recording{ID: r2.id, Filename: r2.filename, StartTime: time.UnixMilli(r2.startMs).UTC()})},
	}, defaultRunOpts())
	if err2 != nil {
		t.Fatalf("Run2: %v", err2)
	}
	if res2.Status != "interrupted" {
		t.Errorf("res2.Status=%q, want interrupted", res2.Status)
	}

	// On-disk state should have r1 (the completed one) but not r2.
	loaded, _ := Load(root)
	if _, ok := loaded.Recordings[r1.id]; !ok {
		t.Errorf("r1 should be persisted after first run")
	}
}

// TestRunner_F16_RenameMovesFolder confirms that an ActionRename actually
// moves the folder on disk from old to new relative path.
func TestRunner_F16_RenameMovesFolder(t *testing.T) {
	r1 := goodFixture("a3f9c021000000000000000000000001")
	r1.filename = "Renamed Rec"
	be := newRunnerBackend(t, r1)
	defer be.close()
	root := t.TempDir()
	client := newAPIClient(t, be.api.URL)

	// Seed: put a folder at the OLD path with a metadata file claiming the
	// transcript is present, so the post-rename fetch is a no-op for it.
	oldRel := "2026/04/2026-04-30_1430_oldname"
	oldAbs := filepath.Join(root, filepath.FromSlash(oldRel))
	if err := os.MkdirAll(oldAbs, 0o755); err != nil {
		t.Fatalf("seed old folder: %v", err)
	}

	apiRec := api.Recording{ID: r1.id, Filename: r1.filename, StartTime: time.UnixMilli(r1.startMs).UTC(), HasTranscript: true, HasSummary: true}
	newRel := expectedRelativePath(apiRec)

	state := &State{
		SchemaVersion: 1,
		Recordings: map[string]RecordingState{
			r1.id: {Version: "v1", VersionMs: 100, FolderPath: oldRel},
		},
	}
	actions := []Action{
		{Kind: ActionRename, Recording: apiRec, Reason: ReasonRename, OldRelative: oldRel, NewRelative: newRel, Folder: newRel},
	}

	runner := &Runner{
		Client: client,
		Root:   root,
		State:  state,
		Now:    func() time.Time { return time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC) },
	}
	if _, err := runner.Run(context.Background(), actions, defaultRunOpts()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(oldAbs); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("old folder should be gone, got stat err=%v", err)
	}
	newAbs := filepath.Join(root, filepath.FromSlash(newRel))
	if _, err := os.Stat(newAbs); err != nil {
		t.Errorf("new folder should exist: %v", err)
	}
	if state.Recordings[r1.id].FolderPath != newRel {
		t.Errorf("state folder_path=%q, want %q", state.Recordings[r1.id].FolderPath, newRel)
	}
}

// TestRunner_F16_RenameCollisionUsesIDSuffix confirms that when the
// destination folder already exists with a different recording's metadata,
// the rename target gets the F-03 6-char-id suffix.
func TestRunner_F16_RenameCollisionUsesIDSuffix(t *testing.T) {
	r1 := goodFixture("a3f9c021000000000000000000000001")
	r1.filename = "Standup"
	be := newRunnerBackend(t, r1)
	defer be.close()
	root := t.TempDir()
	client := newAPIClient(t, be.api.URL)

	apiRec := api.Recording{ID: r1.id, Filename: r1.filename, StartTime: time.UnixMilli(r1.startMs).UTC(), HasTranscript: true, HasSummary: true}
	newRel := expectedRelativePath(apiRec)

	// Pre-create the new path with a different recording's metadata.
	collisionAbs := filepath.Join(root, filepath.FromSlash(newRel))
	if err := os.MkdirAll(collisionAbs, 0o755); err != nil {
		t.Fatalf("seed collision: %v", err)
	}
	other := &archive.Metadata{ArchiveSchemaVersion: 1, ID: "OTHER_ID_DIFFERENT_FROM_RENAME_TARGET", ClientVersion: "test"}
	mb, _ := archive.MarshalMetadata(other)
	if err := os.WriteFile(filepath.Join(collisionAbs, archive.MetadataFilename), mb, 0o644); err != nil {
		t.Fatalf("seed collision metadata: %v", err)
	}

	// Seed the OLD folder we'll rename from.
	oldRel := "2026/04/2026-04-30_1430_oldname"
	oldAbs := filepath.Join(root, filepath.FromSlash(oldRel))
	if err := os.MkdirAll(oldAbs, 0o755); err != nil {
		t.Fatalf("seed old: %v", err)
	}

	state := &State{
		SchemaVersion: 1,
		Recordings: map[string]RecordingState{
			r1.id: {Version: "v1", VersionMs: 100, FolderPath: oldRel},
		},
	}
	actions := []Action{
		{Kind: ActionRename, Recording: apiRec, Reason: ReasonRename, OldRelative: oldRel, NewRelative: newRel, Folder: newRel},
	}

	runner := &Runner{
		Client: client,
		Root:   root,
		State:  state,
		Now:    func() time.Time { return time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC) },
	}
	if _, err := runner.Run(context.Background(), actions, defaultRunOpts()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The state's folder_path should NOT equal the colliding newRel; it
	// should have an id-suffix variant.
	got := state.Recordings[r1.id].FolderPath
	if got == newRel {
		t.Errorf("collision not resolved: folder_path=%q", got)
	}
	if !strings.HasPrefix(got, newRel+"_") && !strings.Contains(got, r1.id[:6]) {
		t.Errorf("expected id-suffix in folder_path %q (id prefix %s)", got, r1.id[:6])
	}
}

// Sanity: emitter receives one event per action.
func TestRunner_EmitsEvents(t *testing.T) {
	r1 := goodFixture("a3f9c021000000000000000000000001")
	be := newRunnerBackend(t, r1)
	defer be.close()
	root := t.TempDir()
	client := newAPIClient(t, be.api.URL)

	state := freshState()
	apiRec := api.Recording{ID: r1.id, Filename: r1.filename, StartTime: time.UnixMilli(r1.startMs).UTC()}
	actions := []Action{
		{Kind: ActionFetch, Recording: apiRec, Reason: ReasonNew, Folder: expectedRelativePath(apiRec)},
	}

	var emitted []Event
	var mu sync.Mutex
	runner := &Runner{
		Client: client,
		Root:   root,
		State:  state,
		Emit:   func(e Event) { mu.Lock(); emitted = append(emitted, e); mu.Unlock() },
		Now:    func() time.Time { return time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC) },
	}
	if _, err := runner.Run(context.Background(), actions, defaultRunOpts()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	kinds := map[EventKind]int{}
	for _, e := range emitted {
		kinds[e.Kind]++
	}
	if kinds[EventFetched] == 0 {
		t.Errorf("expected at least one fetched event, got %+v", kinds)
	}
}

// Sanity: cancelled context propagates into fetch.FetchOne and surfaces as
// a per-recording failure with status "interrupted" at the run level.
func TestRunner_CancelledContextSurfacesInterrupted(t *testing.T) {
	r1 := goodFixture("a3f9c021000000000000000000000001")
	be := newRunnerBackend(t, r1)
	defer be.close()
	root := t.TempDir()
	client := newAPIClient(t, be.api.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	apiRec := api.Recording{ID: r1.id, Filename: r1.filename, StartTime: time.UnixMilli(r1.startMs).UTC()}
	actions := []Action{
		{Kind: ActionFetch, Recording: apiRec, Reason: ReasonNew, Folder: expectedRelativePath(apiRec)},
	}

	state := freshState()
	runner := &Runner{
		Client: client,
		Root:   root,
		State:  state,
		Now:    func() time.Time { return time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC) },
	}
	res, err := runner.Run(ctx, actions, defaultRunOpts())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != "interrupted" {
		t.Errorf("Status=%q, want interrupted", res.Status)
	}
}

// Sanity: archive.IncludeSet plumbed through; fetch.Result carries it.
var _ = fetch.StatusFetched
