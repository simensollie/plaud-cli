package archive

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

func threeSegmentFixture() Transcript {
	return Transcript{
		Version: 1,
		Segments: []Segment{
			{Speaker: "Speaker 0", StartMs: 0, EndMs: 4320, Text: "Velkommen til møtet."},
			{Speaker: "Speaker 1", StartMs: 4320, EndMs: 9100, Text: "Takk for at jeg fikk være med."},
			{Speaker: "Speaker 0", StartMs: 9100, EndMs: 13750, Text: "La oss gå gjennom agendaen."},
		},
	}
}

func emptySpeakerFixture() Transcript {
	return Transcript{
		Version: 1,
		Segments: []Segment{
			{Speaker: "Speaker 0", StartMs: 0, EndMs: 2500, Text: "God morgen."},
			{Speaker: "", StartMs: 2500, EndMs: 5000, Text: "Bakgrunnsstemme i rommet."},
			{Speaker: "Speaker 1", StartMs: 5000, EndMs: 8200, Text: "God morgen, klar for å starte."},
		},
	}
}

func mustRender(t *testing.T, tr Transcript, format string) []byte {
	t.Helper()
	got, err := Render(tr, format)
	if err != nil {
		t.Fatalf("Render(%q) failed: %v", format, err)
	}
	return got
}

func compareGolden(t *testing.T, got []byte, goldenPath string) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden mismatch for %s:\n--- want ---\n%s\n--- got ---\n%s", goldenPath, want, got)
	}
}

func TestRender_F05_TranscriptJSONIsObjectWithVersionField(t *testing.T) {
	tr := threeSegmentFixture()
	data, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		t.Fatalf("expected JSON object, got: %s", data)
	}
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("unmarshal as object: %v", err)
	}
	if _, ok := generic["version"]; !ok {
		t.Fatalf("expected top-level version field, got keys: %v", generic)
	}
	if _, ok := generic["segments"]; !ok {
		t.Fatalf("expected top-level segments field, got keys: %v", generic)
	}
	var versioned struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &versioned); err != nil {
		t.Fatalf("unmarshal version: %v", err)
	}
	if versioned.Version != 1 {
		t.Fatalf("expected version=1, got %d", versioned.Version)
	}
}

func TestRender_F05_StartMsEndMsRoundTripPreservesPrecision(t *testing.T) {
	tr := Transcript{
		Version: 1,
		Segments: []Segment{
			{Speaker: "A", StartMs: 4321, EndMs: 9999, Text: "x"},
			{Speaker: "B", StartMs: 1234567890, EndMs: 1234567891, Text: "y"},
		},
	}
	data, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Transcript
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Segments) != len(tr.Segments) {
		t.Fatalf("segment count mismatch: got %d want %d", len(got.Segments), len(tr.Segments))
	}
	for i := range tr.Segments {
		if got.Segments[i].StartMs != tr.Segments[i].StartMs {
			t.Fatalf("seg %d start: got %d want %d", i, got.Segments[i].StartMs, tr.Segments[i].StartMs)
		}
		if got.Segments[i].EndMs != tr.Segments[i].EndMs {
			t.Fatalf("seg %d end: got %d want %d", i, got.Segments[i].EndMs, tr.Segments[i].EndMs)
		}
	}
}

func TestRender_F05_Markdown_Golden(t *testing.T) {
	got := mustRender(t, threeSegmentFixture(), "md")
	compareGolden(t, got, "testdata/golden/0002/transcript_3segments.md")
}

func TestRender_F05_SRT_Golden(t *testing.T) {
	got := mustRender(t, threeSegmentFixture(), "srt")
	compareGolden(t, got, "testdata/golden/0002/transcript_3segments.srt")
}

func TestRender_F05_VTT_Golden(t *testing.T) {
	got := mustRender(t, threeSegmentFixture(), "vtt")
	compareGolden(t, got, "testdata/golden/0002/transcript_3segments.vtt")
}

func TestRender_F05_PlainText_Golden(t *testing.T) {
	got := mustRender(t, threeSegmentFixture(), "txt")
	compareGolden(t, got, "testdata/golden/0002/transcript_3segments.txt")
}

func TestRender_F05_EmptySpeakerOmitsPrefix(t *testing.T) {
	tr := emptySpeakerFixture()

	mdGot := mustRender(t, tr, "md")
	compareGolden(t, mdGot, "testdata/golden/0002/transcript_emptyspeaker.md")
	if strings.Contains(string(mdGot), "**** ") {
		t.Fatalf("md: empty speaker should not produce empty bold prefix:\n%s", mdGot)
	}

	srtGot := mustRender(t, tr, "srt")
	compareGolden(t, srtGot, "testdata/golden/0002/transcript_emptyspeaker.srt")
	if strings.Contains(string(srtGot), ": Bakgrunnsstemme") {
		t.Fatalf("srt: empty speaker should not produce ': ' prefix:\n%s", srtGot)
	}

	vttGot := mustRender(t, tr, "vtt")
	compareGolden(t, vttGot, "testdata/golden/0002/transcript_emptyspeaker.vtt")
	if strings.Contains(string(vttGot), ": Bakgrunnsstemme") {
		t.Fatalf("vtt: empty speaker should not produce ': ' prefix:\n%s", vttGot)
	}

	txtGot := mustRender(t, tr, "txt")
	compareGolden(t, txtGot, "testdata/golden/0002/transcript_emptyspeaker.txt")
	if strings.Contains(string(txtGot), ": Bakgrunnsstemme") {
		t.Fatalf("txt: empty speaker should not produce ': ' prefix:\n%s", txtGot)
	}
}

func TestRender_F05_RejectsUnknownFormat(t *testing.T) {
	_, err := Render(threeSegmentFixture(), "doc")
	if err == nil {
		t.Fatal("expected error for unknown format, got nil")
	}
	if !strings.Contains(err.Error(), `unknown transcript format: "doc"`) {
		t.Fatalf("error message %q does not match expected wording", err.Error())
	}
}

func TestRender_F05_OriginalSpeakerOmittedWhenEmpty(t *testing.T) {
	tr := Transcript{Version: 1, Segments: []Segment{
		{Speaker: "Simen", StartMs: 0, EndMs: 1000, Text: "Hei."},
	}}
	b, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(b), "original_speaker") {
		t.Fatalf("expected original_speaker to be omitted when empty, got %s", b)
	}
}

func TestRender_F05_OriginalSpeakerPresentWhenSet(t *testing.T) {
	tr := Transcript{Version: 1, Segments: []Segment{
		{Speaker: "Simen", OriginalSpeaker: "Speaker 0", StartMs: 0, EndMs: 1000, Text: "Hei."},
	}}
	b, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(b), `"original_speaker":"Speaker 0"`) {
		t.Fatalf("expected original_speaker key/value to appear, got %s", b)
	}
}
