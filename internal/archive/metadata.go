package archive

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// archiveSchemaVersion bumps only on breaking changes to the metadata.json
// shape or the per-recording folder layout.
const archiveSchemaVersion = 1

// clientVersion identifies the binary that wrote the metadata. Hand-bumped
// on release for now; build-time injection is future work.
const clientVersion = "0.2.0-dev"

// MetadataFilename is the canonical name of the per-recording metadata file.
const MetadataFilename = "metadata.json"

// IncludeSet is the set of artifacts requested for a recording. Used by
// downstream code to gate per-artifact fetching and to populate the
// per-artifact sub-objects of metadata.json.
type IncludeSet struct {
	Audio      bool
	Transcript bool
	Summary    bool
	Metadata   bool
}

// WriteOptions captures invocation-level flags that affect archive writes.
type WriteOptions struct {
	Force       bool
	Include     IncludeSet
	AudioFormat string
}

// Recording is the local input view onto a Plaud recording, used to build a
// Metadata. We keep this independent of internal/api so the archive package
// can be unit-tested without pulling the HTTP layer in.
type Recording struct {
	ID              string
	Title           string
	TitleSlug       string
	Region          string
	RecordedAtUTC   time.Time
	RecordedAtLocal time.Time
	DurationMS      int64
}

// MetaAudio is the audio sub-object of metadata.json.
type MetaAudio struct {
	Filename          string `json:"filename"`
	SizeBytes         int64  `json:"size_bytes"`
	S3ETag            string `json:"s3_etag"`
	OriginalUploadMD5 string `json:"original_upload_md5,omitempty"`
	LocalMD5          string `json:"local_md5"`
	LocalSHA256       string `json:"local_sha256"`
}

// MetaTranscript is the transcript sub-object of metadata.json.
type MetaTranscript struct {
	Filename     string `json:"filename"`
	SHA256       string `json:"sha256"`
	SegmentCount int    `json:"segment_count"`
	Language     string `json:"language,omitempty"`
}

// MetaSummary is the summary sub-object of metadata.json.
type MetaSummary struct {
	Filename string `json:"filename"`
	SHA256   string `json:"sha256"`
}

// Metadata is the in-memory representation of metadata.json. Sub-object
// pointers are nil when the corresponding artifact is not present in the
// folder. F-07, §4.
type Metadata struct {
	ArchiveSchemaVersion int             `json:"archive_schema_version"`
	ClientVersion        string          `json:"client_version"`
	ID                   string          `json:"id"`
	Title                string          `json:"title"`
	TitleSlug            string          `json:"title_slug"`
	Region               string          `json:"plaud_region"`
	RecordedAtUTC        time.Time       `json:"recorded_at_utc"`
	RecordedAtLocal      time.Time       `json:"recorded_at_local"`
	DurationMS           int64           `json:"duration_ms"`
	Audio                *MetaAudio      `json:"audio,omitempty"`
	Transcript           *MetaTranscript `json:"transcript,omitempty"`
	Summary              *MetaSummary    `json:"summary,omitempty"`
	FetchedAt            time.Time       `json:"fetched_at"`
	LastVerifiedAt       time.Time       `json:"last_verified_at"`
}

// NewMetadata builds a Metadata from a Recording, with both timestamps
// initialized to now. Sub-objects are nil; populate them with the
// per-artifact setters as artifacts are written.
func NewMetadata(r Recording, now time.Time) *Metadata {
	return &Metadata{
		ArchiveSchemaVersion: archiveSchemaVersion,
		ClientVersion:        clientVersion,
		ID:                   r.ID,
		Title:                r.Title,
		TitleSlug:            r.TitleSlug,
		Region:               r.Region,
		RecordedAtUTC:        r.RecordedAtUTC.UTC(),
		RecordedAtLocal:      r.RecordedAtLocal,
		DurationMS:           r.DurationMS,
		FetchedAt:            now.UTC(),
		LastVerifiedAt:       now.UTC(),
	}
}

// SetAudio attaches an audio sub-object.
func (m *Metadata) SetAudio(a MetaAudio) { m.Audio = &a }

// SetTranscript attaches a transcript sub-object.
func (m *Metadata) SetTranscript(t MetaTranscript) { m.Transcript = &t }

// SetSummary attaches a summary sub-object.
func (m *Metadata) SetSummary(s MetaSummary) { m.Summary = &s }

// MarkVerified bumps last_verified_at to now. Use on every successful run.
func (m *Metadata) MarkVerified(now time.Time) { m.LastVerifiedAt = now.UTC() }

// MarkArtifactWritten bumps fetched_at to now and last_verified_at if it is
// behind. Call only when an artifact write actually occurred.
func (m *Metadata) MarkArtifactWritten(now time.Time) {
	now = now.UTC()
	m.FetchedAt = now
	if m.LastVerifiedAt.Before(now) {
		m.LastVerifiedAt = now
	}
}

// MarshalMetadata renders m as pretty-printed JSON with sorted keys and a
// trailing newline. F-09c.
func MarshalMetadata(m *Metadata) ([]byte, error) {
	// Round-trip through a generic map so json.Marshal sorts the keys.
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshaling metadata: %w", err)
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("re-decoding metadata: %w", err)
	}
	out, err := json.MarshalIndent(generic, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("re-encoding metadata: %w", err)
	}
	out = append(out, '\n')
	return out, nil
}

// UnmarshalMetadata parses bytes into a Metadata. Returns a non-nil error
// when the JSON is malformed; callers detect "corrupt metadata" via this
// path and fall back to RebuildMetadataFromDisk.
func UnmarshalMetadata(b []byte) (*Metadata, error) {
	var m Metadata
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("decoding metadata: %w", err)
	}
	return &m, nil
}

// ShouldRewriteTranscript reports whether transcript.json should be
// rewritten given the existing metadata's recorded transcript hash and the
// freshly computed hash. F-07.
func ShouldRewriteTranscript(existing *Metadata, freshSHA256 string) bool {
	if existing == nil || existing.Transcript == nil {
		return true
	}
	return existing.Transcript.SHA256 != freshSHA256
}

// ShouldRewriteSummary reports whether summary.plaud.md should be
// rewritten. Same pattern as the transcript predicate. F-07.
func ShouldRewriteSummary(existing *Metadata, freshSHA256 string) bool {
	if existing == nil || existing.Summary == nil {
		return true
	}
	return existing.Summary.SHA256 != freshSHA256
}

// RebuildMetadataFromDisk reconstructs a Metadata by scanning folder for
// audio.<ext>, transcript.json, and summary.plaud.md and hashing whatever
// it finds. The recording argument supplies the bookkeeping fields the
// disk cannot. F-14.
func RebuildMetadataFromDisk(folder string, r Recording, now time.Time) (*Metadata, error) {
	m := NewMetadata(r, now)

	if a, err := scanAudio(folder); err != nil {
		return nil, fmt.Errorf("scanning audio: %w", err)
	} else if a != nil {
		m.SetAudio(*a)
	}

	if t, err := scanTranscript(folder); err != nil {
		return nil, fmt.Errorf("scanning transcript: %w", err)
	} else if t != nil {
		m.SetTranscript(*t)
	}

	if s, err := scanSummary(folder); err != nil {
		return nil, fmt.Errorf("scanning summary: %w", err)
	} else if s != nil {
		m.SetSummary(*s)
	}

	return m, nil
}

func scanAudio(folder string) (*MetaAudio, error) {
	for _, ext := range []string{".mp3", ".m4a", ".wav", ".aac", ".flac", ".ogg"} {
		name := "audio" + ext
		p := filepath.Join(folder, name)
		f, err := os.Open(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("opening %s: %w", p, err)
		}
		defer f.Close()
		md5h := md5.New()
		shah := sha256.New()
		size, err := io.Copy(io.MultiWriter(md5h, shah), f)
		if err != nil {
			return nil, fmt.Errorf("hashing %s: %w", p, err)
		}
		return &MetaAudio{
			Filename:    name,
			SizeBytes:   size,
			LocalMD5:    hex.EncodeToString(md5h.Sum(nil)),
			LocalSHA256: hex.EncodeToString(shah.Sum(nil)),
		}, nil
	}
	return nil, nil
}

func scanTranscript(folder string) (*MetaTranscript, error) {
	p := filepath.Join(folder, "transcript.json")
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", p, err)
	}
	sum := sha256.Sum256(b)
	return &MetaTranscript{
		Filename:     "transcript.json",
		SHA256:       hex.EncodeToString(sum[:]),
		SegmentCount: countSegments(b),
	}, nil
}

func scanSummary(folder string) (*MetaSummary, error) {
	p := filepath.Join(folder, "summary.plaud.md")
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", p, err)
	}
	sum := sha256.Sum256(b)
	return &MetaSummary{
		Filename: "summary.plaud.md",
		SHA256:   hex.EncodeToString(sum[:]),
	}, nil
}

// countSegments returns the length of the `segments` array in a transcript
// JSON document. Returns 0 when the document does not parse or is missing
// the field; full schema validation happens elsewhere (Phase 4 owner).
func countSegments(b []byte) int {
	var doc struct {
		Segments []json.RawMessage `json:"segments"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return 0
	}
	return len(doc.Segments)
}
