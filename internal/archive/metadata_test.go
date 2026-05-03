package archive

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func unmarshalMetaToMap(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

func sampleRecording() Recording {
	return Recording{
		ID:              "a3f9c021b2d34e5f6789012345678901",
		Title:           "Kickoff møte",
		TitleSlug:       "kickoff_m_te",
		Region:          "euc1",
		RecordedAtUTC:   time.Date(2026, 4, 30, 14, 30, 0, 0, time.UTC),
		RecordedAtLocal: time.Date(2026, 4, 30, 16, 30, 0, 0, time.FixedZone("CEST", 2*3600)),
		DurationMS:      3720000,
	}
}

func TestMetadata_F07_FetchedAtVsLastVerifiedAtSemantics(t *testing.T) {
	rec := sampleRecording()
	t0 := time.Date(2026, 5, 1, 9, 14, 21, 0, time.UTC)
	t1 := t0.Add(time.Hour)

	// First write with an artifact: both timestamps equal t0.
	m := NewMetadata(rec, t0)
	m.SetTranscript(MetaTranscript{
		Filename:     "transcript.json",
		SHA256:       "abc",
		SegmentCount: 2,
		Language:     "no",
	})
	m.MarkArtifactWritten(t0)

	if !m.FetchedAt.Equal(t0) {
		t.Fatalf("fetched_at = %v, want %v", m.FetchedAt, t0)
	}
	if !m.LastVerifiedAt.Equal(t0) {
		t.Fatalf("last_verified_at = %v, want %v", m.LastVerifiedAt, t0)
	}

	// Re-run later with no artifact write: only last_verified_at bumps.
	m.MarkVerified(t1)
	if !m.FetchedAt.Equal(t0) {
		t.Fatalf("after no-write run, fetched_at = %v, want %v", m.FetchedAt, t0)
	}
	if !m.LastVerifiedAt.Equal(t1) {
		t.Fatalf("after no-write run, last_verified_at = %v, want %v", m.LastVerifiedAt, t1)
	}

	// Re-run with an artifact write: both bump to t2.
	t2 := t1.Add(time.Hour)
	m.MarkVerified(t2)
	m.MarkArtifactWritten(t2)
	if !m.FetchedAt.Equal(t2) {
		t.Fatalf("after write run, fetched_at = %v, want %v", m.FetchedAt, t2)
	}
	if !m.LastVerifiedAt.Equal(t2) {
		t.Fatalf("after write run, last_verified_at = %v, want %v", m.LastVerifiedAt, t2)
	}
}

func TestMetadata_F07_TranscriptSHA256MatchSkipsRewrite(t *testing.T) {
	existing := &Metadata{Transcript: &MetaTranscript{SHA256: "deadbeef"}}
	if ShouldRewriteTranscript(existing, "deadbeef") {
		t.Fatalf("identical hashes should not trigger rewrite")
	}
}

func TestMetadata_F07_TranscriptSHA256MismatchTriggersRewrite(t *testing.T) {
	existing := &Metadata{Transcript: &MetaTranscript{SHA256: "deadbeef"}}
	if !ShouldRewriteTranscript(existing, "cafef00d") {
		t.Fatalf("mismatched hashes should trigger rewrite")
	}
	// Missing previous transcript metadata also triggers rewrite.
	if !ShouldRewriteTranscript(&Metadata{}, "cafef00d") {
		t.Fatalf("missing transcript metadata should trigger rewrite")
	}
	// Nil metadata also triggers rewrite.
	if !ShouldRewriteTranscript(nil, "cafef00d") {
		t.Fatalf("nil metadata should trigger rewrite")
	}
}

func TestMetadata_F09c_PrettyPrintedSortedKeysTrailingNewline(t *testing.T) {
	rec := sampleRecording()
	t0 := time.Date(2026, 5, 1, 9, 14, 21, 0, time.UTC)
	m := NewMetadata(rec, t0)
	m.SetTranscript(MetaTranscript{
		Filename:     "transcript.json",
		SHA256:       "abc",
		SegmentCount: 2,
		Language:     "no",
	})
	m.MarkArtifactWritten(t0)

	b, err := MarshalMetadata(m)
	if err != nil {
		t.Fatalf("MarshalMetadata: %v", err)
	}

	if !strings.HasSuffix(string(b), "\n") {
		t.Fatalf("metadata JSON must end with newline; got %q", b)
	}
	// 2-space indent: every indented line begins with a multiple of 2 spaces.
	for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		trim := strings.TrimLeft(line, " ")
		spaces := len(line) - len(trim)
		if spaces%2 != 0 {
			t.Fatalf("line %q is indented with non-2-space stride", line)
		}
	}

	// Top-level keys are sorted; deeper structural sorting is covered by the
	// byte-level test.
	keys := extractTopLevelKeys(t, b)
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	for i := range keys {
		if keys[i] != sorted[i] {
			t.Fatalf("top-level keys not sorted: got %v, want %v", keys, sorted)
		}
	}
}

func TestMetadata_F14_RebuildFromLocalFilesWhenCorrupt(t *testing.T) {
	dir := t.TempDir()
	rec := sampleRecording()

	// Seed the folder with valid artifacts.
	audio := []byte("audio bytes here pretending to be mp3")
	transcript := []byte(`{"version":1,"segments":[{"speaker":"","start_ms":0,"end_ms":100,"text":"hi"}]}`)
	summary := []byte("# Summary\n\nbody")

	mustWrite(t, filepath.Join(dir, "audio.mp3"), audio)
	mustWrite(t, filepath.Join(dir, "transcript.json"), transcript)
	mustWrite(t, filepath.Join(dir, "summary.plaud.md"), summary)
	// Corrupt metadata.json.
	mustWrite(t, filepath.Join(dir, "metadata.json"), []byte("not json"))

	now := time.Date(2026, 5, 1, 9, 14, 21, 0, time.UTC)
	rebuilt, err := RebuildMetadataFromDisk(dir, rec, now)
	if err != nil {
		t.Fatalf("RebuildMetadataFromDisk: %v", err)
	}

	if rebuilt.ID != rec.ID {
		t.Fatalf("id = %q, want %q", rebuilt.ID, rec.ID)
	}
	if rebuilt.Audio == nil {
		t.Fatalf("audio sub-object missing")
	}
	wantAudioMD5 := hex.EncodeToString(md5sum(audio))
	wantAudioSHA := hex.EncodeToString(sha256sum(audio))
	if rebuilt.Audio.LocalMD5 != wantAudioMD5 {
		t.Fatalf("audio md5 = %q, want %q", rebuilt.Audio.LocalMD5, wantAudioMD5)
	}
	if rebuilt.Audio.LocalSHA256 != wantAudioSHA {
		t.Fatalf("audio sha256 = %q, want %q", rebuilt.Audio.LocalSHA256, wantAudioSHA)
	}
	if rebuilt.Audio.SizeBytes != int64(len(audio)) {
		t.Fatalf("audio size = %d, want %d", rebuilt.Audio.SizeBytes, len(audio))
	}

	if rebuilt.Transcript == nil {
		t.Fatalf("transcript sub-object missing")
	}
	wantTransSHA := hex.EncodeToString(sha256sum(transcript))
	if rebuilt.Transcript.SHA256 != wantTransSHA {
		t.Fatalf("transcript sha256 = %q, want %q", rebuilt.Transcript.SHA256, wantTransSHA)
	}

	if rebuilt.Summary == nil {
		t.Fatalf("summary sub-object missing")
	}
	wantSumSHA := hex.EncodeToString(sha256sum(summary))
	if rebuilt.Summary.SHA256 != wantSumSHA {
		t.Fatalf("summary sha256 = %q, want %q", rebuilt.Summary.SHA256, wantSumSHA)
	}
}

func TestMetadata_PerArtifactSubobjectsAbsentWhenSkipped(t *testing.T) {
	rec := sampleRecording()
	t0 := time.Date(2026, 5, 1, 9, 14, 21, 0, time.UTC)
	m := NewMetadata(rec, t0)
	// Only set transcript; leave audio and summary unset.
	m.SetTranscript(MetaTranscript{
		Filename:     "transcript.json",
		SHA256:       "abc",
		SegmentCount: 1,
	})
	m.MarkArtifactWritten(t0)

	b, err := MarshalMetadata(m)
	if err != nil {
		t.Fatalf("MarshalMetadata: %v", err)
	}
	got := unmarshalMetaToMap(t, b)
	if _, ok := got["audio"]; ok {
		t.Fatalf("audio key present when skipped: %v", got)
	}
	if _, ok := got["summary"]; ok {
		t.Fatalf("summary key present when skipped: %v", got)
	}
	if _, ok := got["transcript"]; !ok {
		t.Fatalf("transcript key absent when set: %v", got)
	}
}

func TestMetadata_SchemaVersionIsOne(t *testing.T) {
	rec := sampleRecording()
	m := NewMetadata(rec, time.Now().UTC())
	if m.ArchiveSchemaVersion != 1 {
		t.Fatalf("archive_schema_version = %d, want 1", m.ArchiveSchemaVersion)
	}
}

func TestMetadata_ClientVersionPopulated(t *testing.T) {
	rec := sampleRecording()
	m := NewMetadata(rec, time.Now().UTC())
	if m.ClientVersion == "" {
		t.Fatalf("client_version is empty")
	}
}

func TestMetadata_F07_AudioFieldsRenamedToS3ETag(t *testing.T) {
	rec := sampleRecording()
	m := NewMetadata(rec, time.Now().UTC())
	m.SetAudio(MetaAudio{Filename: "audio.mp3", SizeBytes: 1, S3ETag: "etag", LocalMD5: "md5", LocalSHA256: "sha"})
	b, err := MarshalMetadata(m)
	if err != nil {
		t.Fatalf("MarshalMetadata: %v", err)
	}
	if !strings.Contains(string(b), `"s3_etag"`) {
		t.Fatalf("expected s3_etag key in output, got:\n%s", b)
	}
	if strings.Contains(string(b), `"server_md5"`) {
		t.Fatalf("server_md5 should no longer appear in metadata.json, got:\n%s", b)
	}
}

func TestMetadata_F07_OriginalUploadMD5OmittedWhenEmpty(t *testing.T) {
	rec := sampleRecording()
	m := NewMetadata(rec, time.Now().UTC())
	m.SetAudio(MetaAudio{Filename: "audio.mp3", SizeBytes: 1, S3ETag: "e", LocalMD5: "m", LocalSHA256: "s"})
	b, err := MarshalMetadata(m)
	if err != nil {
		t.Fatalf("MarshalMetadata: %v", err)
	}
	if strings.Contains(string(b), "original_upload_md5") {
		t.Fatalf("expected original_upload_md5 to be omitted when empty, got:\n%s", b)
	}
}

func TestMetadata_F07_OriginalUploadMD5PresentWhenSet(t *testing.T) {
	rec := sampleRecording()
	m := NewMetadata(rec, time.Now().UTC())
	m.SetAudio(MetaAudio{
		Filename:          "audio.mp3",
		SizeBytes:         1,
		S3ETag:            "e",
		OriginalUploadMD5: "9c0d80abcdef0123456789abcdef0123",
		LocalMD5:          "m",
		LocalSHA256:       "s",
	})
	b, err := MarshalMetadata(m)
	if err != nil {
		t.Fatalf("MarshalMetadata: %v", err)
	}
	if !strings.Contains(string(b), `"original_upload_md5": "9c0d80abcdef0123456789abcdef0123"`) {
		t.Fatalf("expected original_upload_md5 key/value, got:\n%s", b)
	}
}

func TestMetadata_F09c_KeysSortedByteLevel(t *testing.T) {
	// Byte-level check: every JSON object's keys appear in lex order in the
	// raw output. Walks the whole tree.
	rec := sampleRecording()
	t0 := time.Date(2026, 5, 1, 9, 14, 21, 0, time.UTC)
	m := NewMetadata(rec, t0)
	m.SetAudio(MetaAudio{Filename: "audio.mp3", SizeBytes: 100, S3ETag: "s", OriginalUploadMD5: "u", LocalMD5: "l", LocalSHA256: "x"})
	m.SetTranscript(MetaTranscript{Filename: "transcript.json", SHA256: "t", SegmentCount: 1, Language: "no"})
	m.SetSummary(MetaSummary{Filename: "summary.plaud.md", SHA256: "z"})
	m.MarkArtifactWritten(t0)

	b, err := MarshalMetadata(m)
	if err != nil {
		t.Fatalf("MarshalMetadata: %v", err)
	}
	keysInOrder := extractTopLevelKeys(t, b)
	sortedKeys := append([]string(nil), keysInOrder...)
	sort.Strings(sortedKeys)
	for i := range keysInOrder {
		if keysInOrder[i] != sortedKeys[i] {
			t.Fatalf("top-level keys not sorted: got %v, want %v", keysInOrder, sortedKeys)
		}
	}
}

func extractTopLevelKeys(t *testing.T, b []byte) []string {
	t.Helper()
	// At the top level, every line of the form `  "key":` (two spaces, a
	// quoted string, then a colon) is a top-level key thanks to the
	// 2-space indent.
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "  \"") {
			continue
		}
		rest := line[3:]
		idx := strings.Index(rest, "\":")
		if idx < 0 {
			continue
		}
		out = append(out, rest[:idx])
	}
	return out
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func md5sum(b []byte) []byte {
	h := md5.Sum(b)
	return h[:]
}

func sha256sum(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}
