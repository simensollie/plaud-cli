package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// detailFixture builds a `data` object with the fields Detail consumes. The
// API host's response wraps it in the standard `{status, msg, data}` envelope.
type detailFixture struct {
	FileID       string
	FileName     string
	LanguageCode string
	IsTrash      bool
	StartTimeMs  int64
	DurationMs   int64

	// Optional artifact entries. Empty values produce omitted entries.
	TranscriptURL string
	TranscriptOK  bool
	SummaryURL    string
	SummaryOK     bool
	SummaryInline string
}

func (f detailFixture) marshal(t *testing.T) []byte {
	t.Helper()

	contentList := []map[string]any{}
	preDownload := []map[string]any{}

	const summaryDataID = "auto_sum:abcd:" + "deadbeef"

	if f.TranscriptURL != "" {
		taskStatus := 0
		if f.TranscriptOK {
			taskStatus = 1
		}
		contentList = append(contentList, map[string]any{
			"data_id":       "source_transaction:6bf84df9:" + f.FileID,
			"data_type":     "transaction",
			"task_status":   taskStatus,
			"data_link":     f.TranscriptURL,
			"err_code":      "",
			"err_msg":       "",
			"data_title":    "",
			"data_tab_name": "Transcript",
			"extra":         map[string]any{"task_id": "20260430194349-v3@deadbeef"},
		})
	}
	if f.SummaryURL != "" || f.SummaryInline != "" {
		taskStatus := 0
		if f.SummaryOK {
			taskStatus = 1
		}
		contentList = append(contentList, map[string]any{
			"data_id":       summaryDataID,
			"data_type":     "auto_sum_note",
			"task_status":   taskStatus,
			"data_link":     f.SummaryURL,
			"err_code":      "",
			"err_msg":       "",
			"data_title":    "",
			"data_tab_name": "Summary",
			"extra":         map[string]any{"summary_id": "abc"},
		})
	}
	if f.SummaryInline != "" {
		preDownload = append(preDownload, map[string]any{
			"data_id":      summaryDataID,
			"data_content": f.SummaryInline,
		})
	}

	data := map[string]any{
		"file_id":                   f.FileID,
		"file_name":                 f.FileName,
		"is_trash":                  f.IsTrash,
		"start_time":                f.StartTimeMs,
		"duration":                  f.DurationMs,
		"content_list":              contentList,
		"pre_download_content_list": preDownload,
		"extra_data": map[string]any{
			"aiContentHeader": map[string]any{
				"language_code": f.LanguageCode,
			},
		},
	}

	body, err := json.Marshal(map[string]any{
		"status": 0,
		"msg":    "success",
		"data":   data,
	})
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return body
}

// segmentJSON marshals Plaud's wire-shape transcript array (not our canonical
// shape) for content-storage stub responses.
func segmentsJSON(t *testing.T, segs []map[string]any) []byte {
	t.Helper()
	body, err := json.Marshal(segs)
	if err != nil {
		t.Fatalf("marshal segments: %v", err)
	}
	return body
}

// TestDetail_F01_ParsesTopLevelEnvelope checks that the detail endpoint's
// envelope is parsed and basic top-level fields land on RecordingDetail.
func TestDetail_F01_ParsesTopLevelEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/file/detail/") {
			http.NotFound(w, r)
			return
		}
		fix := detailFixture{
			FileID:       "abc123",
			FileName:     "Kickoff",
			LanguageCode: "no",
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fix.marshal(t))
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionEU, "tok", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := c.Detail(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if got.ID != "abc123" {
		t.Errorf("ID: got %q, want %q", got.ID, "abc123")
	}
	if got.Title != "Kickoff" {
		t.Errorf("Title: got %q, want %q", got.Title, "Kickoff")
	}
	if got.Language != "no" {
		t.Errorf("Language: got %q, want %q", got.Language, "no")
	}
}

// TestDetail_F01_PicksTranscriptArtifactFromContentList checks that the
// `transaction` entry's data_link is fetched and decoded from Plaud's wire
// shape into canonical Segments.
func TestDetail_F01_PicksTranscriptArtifactFromContentList(t *testing.T) {
	// Content-storage server: serves the transcript JSON.
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("S3 leg received Authorization header (F-13)")
		}
		segs := []map[string]any{
			{"start_time": 0, "end_time": 4320, "content": "Hei alle sammen", "speaker": "Simen", "original_speaker": "Speaker 0"},
			{"start_time": 4320, "end_time": 9100, "content": "Velkommen", "speaker": "Speaker 1", "original_speaker": "Speaker 1"},
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(segmentsJSON(t, segs))
	}))
	t.Cleanup(storage.Close)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fix := detailFixture{
			FileID:        "abc123",
			FileName:      "Kickoff",
			LanguageCode:  "no",
			TranscriptURL: storage.URL + "/transcript.json.gz",
			TranscriptOK:  true,
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fix.marshal(t))
	}))
	t.Cleanup(api.Close)

	c, err := New(RegionEU, "tok", WithBaseURL(api.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := c.Detail(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if len(got.Segments) != 2 {
		t.Fatalf("len(Segments) = %d, want 2", len(got.Segments))
	}
	if got.Segments[0].Speaker != "Simen" {
		t.Errorf("Segments[0].Speaker = %q, want %q", got.Segments[0].Speaker, "Simen")
	}
	if got.Segments[0].StartMs != 0 || got.Segments[0].EndMs != 4320 {
		t.Errorf("Segments[0] times: got start=%d end=%d, want 0/4320", got.Segments[0].StartMs, got.Segments[0].EndMs)
	}
	if got.Segments[0].Text != "Hei alle sammen" {
		t.Errorf("Segments[0].Text = %q, want %q", got.Segments[0].Text, "Hei alle sammen")
	}
}

// TestDetail_F01_PrefersInlinedSummaryOverContentLink checks that when
// pre_download_content_list inlines the summary, the helper does not call out
// to the auto_sum_note signed URL.
func TestDetail_F01_PrefersInlinedSummaryOverContentLink(t *testing.T) {
	// This server must NEVER be hit. Failing the test if it is.
	never := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("inlined summary should not trigger a fetch; got %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(never.Close)

	const inlined = "## Møteinformasjon\n\nSynthetic summary."

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fix := detailFixture{
			FileID:        "abc123",
			FileName:      "Kickoff",
			LanguageCode:  "no",
			SummaryURL:    never.URL + "/summary.md.gz",
			SummaryOK:     true,
			SummaryInline: inlined,
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fix.marshal(t))
	}))
	t.Cleanup(api.Close)

	c, err := New(RegionEU, "tok", WithBaseURL(api.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := c.Detail(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if got.Summary != inlined {
		t.Errorf("Summary: got %q, want %q", got.Summary, inlined)
	}
}

// TestDetail_F01_FallsBackToSummaryContentLinkWhenNotInlined checks that the
// auto_sum_note data_link is fetched when no inlined entry exists. The bytes
// at that URL are markdown verbatim.
func TestDetail_F01_FallsBackToSummaryContentLinkWhenNotInlined(t *testing.T) {
	const summaryMD = "## Møteinformasjon\n\nFetched summary."

	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("S3 leg received Authorization header (F-13)")
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(summaryMD))
	}))
	t.Cleanup(storage.Close)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fix := detailFixture{
			FileID:     "abc123",
			FileName:   "Kickoff",
			SummaryURL: storage.URL + "/summary.md.gz",
			SummaryOK:  true,
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fix.marshal(t))
	}))
	t.Cleanup(api.Close)

	c, err := New(RegionEU, "tok", WithBaseURL(api.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := c.Detail(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if got.Summary != summaryMD {
		t.Errorf("Summary: got %q, want %q", got.Summary, summaryMD)
	}
}

// TestDetail_F05_MapsPlaudWireShapeToCanonicalSegments verifies the key
// renames from Plaud's `start_time/end_time/content` to our `start_ms/end_ms/text`.
func TestDetail_F05_MapsPlaudWireShapeToCanonicalSegments(t *testing.T) {
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		segs := []map[string]any{
			{"start_time": 0, "end_time": 1000, "content": "one", "speaker": "A", "original_speaker": "Speaker 0"},
			{"start_time": 1000, "end_time": 2000, "content": "two", "speaker": "B", "original_speaker": "Speaker 1"},
			{"start_time": 2000, "end_time": 3000, "content": "three", "speaker": "C", "original_speaker": "Speaker 2"},
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(segmentsJSON(t, segs))
	}))
	t.Cleanup(storage.Close)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fix := detailFixture{
			FileID:        "abc123",
			FileName:      "Kickoff",
			TranscriptURL: storage.URL + "/transcript.json.gz",
			TranscriptOK:  true,
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fix.marshal(t))
	}))
	t.Cleanup(api.Close)

	c, err := New(RegionEU, "tok", WithBaseURL(api.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := c.Detail(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if len(got.Segments) != 3 {
		t.Fatalf("len = %d, want 3", len(got.Segments))
	}
	want := []Segment{
		{Speaker: "A", OriginalSpeaker: "Speaker 0", StartMs: 0, EndMs: 1000, Text: "one"},
		{Speaker: "B", OriginalSpeaker: "Speaker 1", StartMs: 1000, EndMs: 2000, Text: "two"},
		{Speaker: "C", OriginalSpeaker: "Speaker 2", StartMs: 2000, EndMs: 3000, Text: "three"},
	}
	for i, w := range want {
		if got.Segments[i] != w {
			t.Errorf("Segments[%d] = %+v, want %+v", i, got.Segments[i], w)
		}
	}
}

// TestDetail_F05_OmitsOriginalSpeakerWhenEqualToSpeaker checks that when the
// raw label and edited label match, OriginalSpeaker is empty in the canonical
// shape (so `omitempty` drops the JSON key downstream).
func TestDetail_F05_OmitsOriginalSpeakerWhenEqualToSpeaker(t *testing.T) {
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		segs := []map[string]any{
			{"start_time": 0, "end_time": 1000, "content": "x", "speaker": "Speaker 0", "original_speaker": "Speaker 0"},
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(segmentsJSON(t, segs))
	}))
	t.Cleanup(storage.Close)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fix := detailFixture{
			FileID:        "abc123",
			FileName:      "Kickoff",
			TranscriptURL: storage.URL + "/transcript.json.gz",
			TranscriptOK:  true,
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fix.marshal(t))
	}))
	t.Cleanup(api.Close)

	c, err := New(RegionEU, "tok", WithBaseURL(api.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := c.Detail(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if got.Segments[0].OriginalSpeaker != "" {
		t.Errorf("OriginalSpeaker: got %q, want empty (equal-to-speaker rule)", got.Segments[0].OriginalSpeaker)
	}
	if got.Segments[0].Speaker != "Speaker 0" {
		t.Errorf("Speaker: got %q, want %q", got.Segments[0].Speaker, "Speaker 0")
	}
}

// TestDetail_F01_PopulatesLanguageFromAIContentHeader verifies the
// extra_data.aiContentHeader.language_code path.
func TestDetail_F01_PopulatesLanguageFromAIContentHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fix := detailFixture{
			FileID:       "abc123",
			FileName:     "Kickoff",
			LanguageCode: "nb-NO",
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fix.marshal(t))
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionEU, "tok", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := c.Detail(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if got.Language != "nb-NO" {
		t.Errorf("Language: got %q, want %q", got.Language, "nb-NO")
	}
}

// TestDetail_F19_ReturnsEmptySegmentsWhenTranscriptionNotReady covers the
// "no `transaction` entry" branch: Detail returns RecordingDetail with
// Segments == nil, no error.
func TestDetail_F19_ReturnsEmptySegmentsWhenTranscriptionNotReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fix := detailFixture{
			FileID:   "abc123",
			FileName: "Fresh",
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fix.marshal(t))
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionEU, "tok", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := c.Detail(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("Detail returned err = %v, want nil (F-19)", err)
	}
	if len(got.Segments) != 0 {
		t.Errorf("Segments: got len=%d, want 0", len(got.Segments))
	}
}

// TestDetail_F19_ReturnsEmptySummaryWhenSummaryNotReady covers the same
// pattern for summary: no auto_sum_note, no inlined entry, no error.
func TestDetail_F19_ReturnsEmptySummaryWhenSummaryNotReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fix := detailFixture{
			FileID:   "abc123",
			FileName: "Fresh",
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fix.marshal(t))
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionEU, "tok", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := c.Detail(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("Detail returned err = %v, want nil (F-19)", err)
	}
	if got.Summary != "" {
		t.Errorf("Summary: got %q, want empty", got.Summary)
	}
}

// TestDetail_F10_Surfaces401 verifies that an HTTP 401 from /file/detail is
// surfaced as ErrUnauthorized so the CLI can render the "log in again" message.
func TestDetail_F10_Surfaces401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionEU, "tok", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = c.Detail(context.Background(), "abc123")
	if err == nil {
		t.Fatal("Detail against 401 returned nil error")
	}
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("err = %v, want errors.Is ErrUnauthorized", err)
	}
}

// TestDetail_F10_NonZeroStatusReturnsAPIError verifies a non-zero envelope
// status (HTTP 200 but logical failure) wraps ErrAPIError.
func TestDetail_F10_NonZeroStatusReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":4,"msg":"not authorised","data":null}`)
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionEU, "tok", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = c.Detail(context.Background(), "abc123")
	if err == nil {
		t.Fatal("Detail with non-zero envelope status returned nil error")
	}
	if !errors.Is(err, ErrAPIError) {
		t.Errorf("err = %v, want errors.Is ErrAPIError", err)
	}
}
