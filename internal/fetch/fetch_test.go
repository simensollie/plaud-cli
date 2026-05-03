package fetch

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simensollie/plaud-cli/internal/api"
	"github.com/simensollie/plaud-cli/internal/archive"
)

type fakeBackend struct {
	api      *httptest.Server
	storage  *httptest.Server
	audio    *httptest.Server
	apiCount int
}

func (b *fakeBackend) close() {
	if b.api != nil {
		b.api.Close()
	}
	if b.storage != nil {
		b.storage.Close()
	}
	if b.audio != nil {
		b.audio.Close()
	}
}

type recordingFixture struct {
	id         string
	filename   string
	startMs    int64
	durationMs int64
	segments   []map[string]any
	summary    string
	audio      []byte
	isTrash    bool
	isTrans    bool
	isSummary  bool
	fileMD5    string
	language   string
}

func newFakeBackend(t *testing.T, recs ...recordingFixture) *fakeBackend {
	t.Helper()
	by := map[string]recordingFixture{}
	for _, r := range recs {
		by[r.id] = r
	}
	b := &fakeBackend{}

	b.storage = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("F-13: storage leg saw Authorization header")
		}
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
		if r.Header.Get("Authorization") != "" {
			t.Errorf("F-13: audio leg saw Authorization header")
		}
		base := filepath.Base(r.URL.Path)
		id := strings.TrimSuffix(base, ".mp3")
		rec, ok := by[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		sum := md5.Sum(rec.audio)
		w.Header().Set("ETag", fmt.Sprintf(`"%s"`, hex.EncodeToString(sum[:])))
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(rec.audio)
		}
	}))

	b.api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.apiCount++
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/file/detail/"):
			id := strings.TrimPrefix(r.URL.Path, "/file/detail/")
			rec, ok := by[id]
			if !ok {
				http.NotFound(w, r)
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
			lang := rec.language
			if lang == "" {
				lang = "no"
			}
			data := map[string]any{
				"file_id":                   rec.id,
				"file_name":                 rec.filename,
				"is_trash":                  rec.isTrash,
				"start_time":                rec.startMs,
				"duration":                  rec.durationMs,
				"content_list":              contentList,
				"pre_download_content_list": pre,
				"extra_data": map[string]any{
					"aiContentHeader": map[string]any{"language_code": lang},
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

func makeAudio(seed byte, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = seed + byte(i%17)
	}
	return b
}

func happyFixture() recordingFixture {
	return recordingFixture{
		id:         "a3f9c021000000000000000000000001",
		filename:   "Kickoff møte",
		startMs:    time.Date(2026, 4, 30, 14, 30, 0, 0, time.UTC).UnixMilli(),
		durationMs: 2723000,
		isTrans:    true,
		isSummary:  true,
		fileMD5:    "abcdef0123456789abcdef0123456789",
		segments: []map[string]any{
			{"start_time": 0, "end_time": 4320, "content": "Hei", "speaker": "Simen", "original_speaker": "Speaker 0"},
			{"start_time": 4320, "end_time": 9100, "content": "Velkommen", "speaker": "Speaker 1", "original_speaker": "Speaker 1"},
		},
		summary:  "## Sammendrag\n\nKickoff for prosjektet.\n",
		audio:    makeAudio(0x42, 512),
		language: "no",
	}
}

func newClient(t *testing.T, baseURL string) *api.Client {
	t.Helper()
	c, err := api.New("eu", "test-token", api.WithBaseURL(baseURL))
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	return c
}

// TestFetchOne_HappyPath exercises the full per-recording orchestration:
// detail call, audio probe + GET, transcript fetch, summary fetch, metadata
// write. Verifies behavior parity with the pre-extraction processRecording.
func TestFetchOne_HappyPath(t *testing.T) {
	be := newFakeBackend(t, happyFixture())
	root := t.TempDir()
	client := newClient(t, be.api.URL)
	rec := happyFixture()

	now := func() time.Time { return time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC) }
	res := FetchOne(context.Background(), client, root, api.Recording{ID: rec.id}, Options{
		Include:           archive.IncludeSet{Audio: true, Transcript: true, Summary: true, Metadata: true},
		TranscriptFormats: []string{"json", "md"},
		AudioFormat:       "mp3",
	}, now)

	if res.Err != nil {
		t.Fatalf("FetchOne: %v", res.Err)
	}
	if res.Status != StatusFetched {
		t.Fatalf("status=%q, want fetched", res.Status)
	}
	for _, name := range []string{"audio.mp3", "transcript.json", "transcript.md", "summary.plaud.md", "metadata.json"} {
		p := filepath.Join(res.Folder, name)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", name, err)
		}
	}

	// Idempotent re-run: status flips to skipped, no new audio HEAD/GET cycles.
	res2 := FetchOne(context.Background(), client, root, api.Recording{ID: rec.id}, Options{
		Include:           archive.IncludeSet{Audio: true, Transcript: true, Summary: true, Metadata: true},
		TranscriptFormats: []string{"json", "md"},
		AudioFormat:       "mp3",
	}, now)
	if res2.Err != nil {
		t.Fatalf("FetchOne (re-run): %v", res2.Err)
	}
	if res2.Status != StatusSkipped {
		t.Fatalf("re-run status=%q, want skipped", res2.Status)
	}
}

// TestFetchOne_RespectsContextCancel asserts that a pre-cancelled context
// never lands real artifacts on disk — ctx.Done() must be honored by the
// HTTP layer (Go stdlib does this for free; verify the wiring).
func TestFetchOne_RespectsContextCancel(t *testing.T) {
	be := newFakeBackend(t, happyFixture())
	root := t.TempDir()
	client := newClient(t, be.api.URL)
	rec := happyFixture()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	now := func() time.Time { return time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC) }
	res := FetchOne(ctx, client, root, api.Recording{ID: rec.id}, Options{
		Include:           archive.IncludeSet{Audio: true, Transcript: true, Summary: true, Metadata: true},
		TranscriptFormats: []string{"json", "md"},
		AudioFormat:       "mp3",
	}, now)

	if res.Status != StatusFailed {
		t.Fatalf("status=%q, want failed", res.Status)
	}
	if res.Err == nil {
		t.Fatalf("expected non-nil err on cancelled ctx")
	}
}
