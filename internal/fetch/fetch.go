// Package fetch is the per-recording fetch primitive shared by the
// `plaud download` command (spec 0002) and `plaud sync` (spec 0003).
//
// Behavior is identical to the body of cmd/plaud/download.go::processRecording
// before the spec 0003 Phase 0a extraction; this package was created so sync
// could call the same code path without duplicating it. The archive package
// is intentionally HTTP-free, so the fetch primitive lives one layer up.
package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/simensollie/plaud-cli/internal/api"
	"github.com/simensollie/plaud-cli/internal/archive"
)

// Status enumerates the possible per-recording outcomes of a single FetchOne
// call. Stable strings; downstream emitters (download's --format json, sync's
// NDJSON events) match against them.
type Status string

const (
	StatusFetched Status = "fetched"
	StatusSkipped Status = "skipped"
	StatusFailed  Status = "failed"
)

// Options captures the per-call knobs FetchOne respects. Mirrors the
// effective per-recording configuration the download command computes from
// flags + env vars; sync constructs the same struct from its own resolution.
type Options struct {
	Include           archive.IncludeSet
	TranscriptFormats []string
	AudioFormat       string
	Force             bool
	// Slug, when non-empty, overrides the slug FetchOne would derive from
	// rec.Filename. Used by spec 0003 sync's rename path to land the fetch
	// under a collision-resolved leaf (F-16) rather than the bare slug.
	Slug string
}

// Result is one recording's fetch outcome. Files lists artifacts written
// this call (plus sentinel "()" notices that emitters interpret); Skipped
// lists artifacts considered but skipped by per-artifact idempotency.
// Sentinels: "(trashed)", "(transcript-not-ready)", "(summary-not-ready)",
// "(metadata-rebuilt)".
type Result struct {
	ID         string
	Status     Status
	Folder     string
	Files      []string
	Skipped    []string
	DurationMs int64
	Err        error
}

// FetchOne fetches one recording into the archive root. It is the single
// per-recording orchestration shared by `plaud download` and `plaud sync`.
//
// Behavior follows spec 0002:
//   - F-07 per-artifact idempotency (audio HEAD ETag, transcript SHA-256,
//     summary SHA-256).
//   - F-14 atomic per-file writes via .partial sweep + tmp+rename.
//   - F-15 two-step audio fetch with one signed-URL refetch on 401/403.
//   - F-19 partial server state (is_trans=false, is_summary=false) surfaces
//     as "()" sentinels in Files; the run does not fail.
func FetchOne(
	ctx context.Context,
	client *api.Client,
	root string,
	rec api.Recording,
	opts Options,
	now func() time.Time,
) Result {
	start := now()
	res := Result{ID: rec.ID}

	detail, err := client.Detail(ctx, rec.ID)
	if err != nil {
		return failResult(res, err, start, now)
	}

	if rec.Filename == "" {
		rec.Filename = detail.Title
	}
	if rec.StartTime.IsZero() {
		rec.StartTime = detail.StartTime
	}
	if rec.Duration == 0 {
		rec.Duration = detail.Duration
	}
	if detail.IsTrash {
		rec.IsTrash = true
	}

	slug := opts.Slug
	if slug == "" {
		slug = archive.Slug(rec.Filename)
		if slug == "untitled" {
			slug = archive.SlugWithCollision(rec.Filename, rec.ID, func(s string) bool { return s == "untitled" })
		}
	}
	archiveRec := archive.Recording{
		ID:              rec.ID,
		Title:           rec.Filename,
		TitleSlug:       slug,
		RecordedAtUTC:   rec.StartTime.UTC(),
		RecordedAtLocal: rec.StartTime,
		DurationMS:      rec.Duration.Milliseconds(),
	}
	folder, err := archive.RecordingFolder(root, archiveRec)
	if err != nil {
		return failResult(res, fmt.Errorf("resolving folder: %w", err), start, now)
	}
	folder = archive.PrefixLongPath(folder)
	res.Folder = folder

	if rec.IsTrash {
		res.Files = append(res.Files, "(trashed)")
	}

	if err := os.MkdirAll(folder, 0o755); err != nil {
		return failResult(res, fmt.Errorf("creating folder: %w", err), start, now)
	}
	if err := archive.SweepPartials(folder); err != nil {
		return failResult(res, fmt.Errorf("sweeping partials: %w", err), start, now)
	}

	meta, rebuilt, err := loadOrInitMetadata(folder, archiveRec, now())
	if err != nil {
		return failResult(res, err, start, now)
	}
	if rebuilt {
		res.Files = append(res.Files, "(metadata-rebuilt)")
	}

	anyWrite := false
	skippedAll := true

	if opts.Include.Audio {
		written, skipped, audioErr := fetchAudio(ctx, client, folder, opts.AudioFormat, rec, meta, opts.Force)
		if audioErr != nil {
			return failResult(res, audioErr, start, now)
		}
		audioName := "audio." + opts.AudioFormat
		if written {
			anyWrite = true
			res.Files = append(res.Files, audioName)
		} else if skipped {
			res.Skipped = append(res.Skipped, audioName)
		}
		if !skipped {
			skippedAll = false
		}
	}

	if opts.Include.Transcript {
		switch {
		case detail == nil || detail.Segments == nil:
			res.Files = append(res.Files, "(transcript-not-ready)")
		default:
			written, transErr := writeTranscript(folder, detail, opts.TranscriptFormats, meta, opts.Force)
			if transErr != nil {
				return failResult(res, transErr, start, now)
			}
			if written {
				anyWrite = true
				skippedAll = false
				res.Files = append(res.Files, "transcript.json")
				for _, fmtName := range opts.TranscriptFormats {
					if fmtName == "json" {
						continue
					}
					res.Files = append(res.Files, "transcript."+fmtName)
				}
			} else {
				res.Skipped = append(res.Skipped, "transcript.json")
				for _, fmtName := range opts.TranscriptFormats {
					if fmtName == "json" {
						continue
					}
					res.Skipped = append(res.Skipped, "transcript."+fmtName)
				}
			}
		}
	}

	if opts.Include.Summary {
		switch {
		case detail == nil || strings.TrimSpace(detail.Summary) == "":
			res.Files = append(res.Files, "(summary-not-ready)")
		default:
			written, sumErr := writeSummary(folder, detail.Summary, meta, opts.Force)
			if sumErr != nil {
				return failResult(res, sumErr, start, now)
			}
			if written {
				anyWrite = true
				skippedAll = false
				res.Files = append(res.Files, "summary.plaud.md")
			} else {
				res.Skipped = append(res.Skipped, "summary.plaud.md")
			}
		}
	}

	if opts.Force {
		meta.MarkArtifactWritten(now())
	} else if anyWrite {
		meta.MarkArtifactWritten(now())
	} else {
		meta.MarkVerified(now())
	}
	metaBytes, err := archive.MarshalMetadata(meta)
	if err != nil {
		return failResult(res, fmt.Errorf("marshaling metadata: %w", err), start, now)
	}
	if err := archive.WriteAtomic(filepath.Join(folder, archive.MetadataFilename), metaBytes); err != nil {
		return failResult(res, fmt.Errorf("writing metadata: %w", err), start, now)
	}
	res.Files = append(res.Files, archive.MetadataFilename)

	res.DurationMs = now().Sub(start).Milliseconds()
	switch {
	case opts.Force:
		res.Status = StatusFetched
	case !anyWrite && skippedAll:
		res.Status = StatusSkipped
	default:
		res.Status = StatusFetched
	}
	return res
}

func failResult(res Result, err error, start time.Time, now func() time.Time) Result {
	res.Status = StatusFailed
	res.Err = err
	res.DurationMs = now().Sub(start).Milliseconds()
	return res
}

func loadOrInitMetadata(folder string, rec archive.Recording, now time.Time) (*archive.Metadata, bool, error) {
	p := filepath.Join(folder, archive.MetadataFilename)
	raw, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return archive.NewMetadata(rec, now), false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("reading %s: %w", p, err)
	}
	m, err := archive.UnmarshalMetadata(raw)
	if err != nil {
		rebuilt, rerr := archive.RebuildMetadataFromDisk(folder, rec, now)
		if rerr != nil {
			return nil, false, fmt.Errorf("rebuilding metadata: %w", rerr)
		}
		return rebuilt, true, nil
	}
	return m, false, nil
}

func fetchAudio(
	ctx context.Context,
	client *api.Client,
	folder string,
	audioFormat string,
	rec api.Recording,
	meta *archive.Metadata,
	force bool,
) (bool, bool, error) {
	signedURL, err := client.TempURL(ctx, rec.ID)
	if err != nil {
		return false, false, fmt.Errorf("fetching audio URL: %w", err)
	}

	probe, err := client.ProbeAudio(ctx, signedURL)
	if errors.Is(err, api.ErrSignedURLExpired) {
		signedURL, err = client.TempURL(ctx, rec.ID)
		if err != nil {
			return false, false, fmt.Errorf("refetching audio URL: %w", err)
		}
		probe, err = client.ProbeAudio(ctx, signedURL)
		if err != nil {
			return false, false, fmt.Errorf("probing audio after retry: %w", err)
		}
	} else if err != nil {
		return false, false, fmt.Errorf("probing audio: %w", err)
	}

	if !force && meta.Audio != nil && probe.ETag != "" && probe.ETag == meta.Audio.S3ETag {
		return false, true, nil
	}

	audioName := "audio." + audioFormat
	dst := filepath.Join(folder, audioName)
	tmp := dst + ".partial"
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return false, false, fmt.Errorf("creating folder for audio: %w", err)
	}
	f, err := os.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return false, false, fmt.Errorf("opening %s: %w", tmp, err)
	}
	hashSHA := sha256.New()
	mw := io.MultiWriter(f, hashSHA)
	written, etag, localMD5, dlErr := client.DownloadAudio(ctx, signedURL, mw)
	if errors.Is(dlErr, api.ErrSignedURLExpired) {
		_ = f.Close()
		_ = os.Remove(tmp)
		signedURL, err = client.TempURL(ctx, rec.ID)
		if err != nil {
			return false, false, fmt.Errorf("refetching audio URL after stream expiry: %w", err)
		}
		f, err = os.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return false, false, fmt.Errorf("re-opening %s: %w", tmp, err)
		}
		hashSHA = sha256.New()
		mw = io.MultiWriter(f, hashSHA)
		written, etag, localMD5, dlErr = client.DownloadAudio(ctx, signedURL, mw)
	}
	if dlErr != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return false, false, fmt.Errorf("downloading audio: %w", dlErr)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return false, false, fmt.Errorf("fsync audio: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return false, false, fmt.Errorf("closing audio: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return false, false, fmt.Errorf("renaming audio: %w", err)
	}

	meta.SetAudio(archive.MetaAudio{
		Filename:          audioName,
		SizeBytes:         written,
		S3ETag:            etag,
		OriginalUploadMD5: rec.FileMD5,
		LocalMD5:          localMD5,
		LocalSHA256:       hex.EncodeToString(hashSHA.Sum(nil)),
	})
	return true, false, nil
}

func writeTranscript(
	folder string,
	detail *api.RecordingDetail,
	formats []string,
	meta *archive.Metadata,
	force bool,
) (bool, error) {
	tr := archive.Transcript{Version: 1, Segments: convertSegments(detail.Segments)}
	if tr.Segments == nil {
		tr.Segments = []archive.Segment{}
	}
	canonical, err := marshalCanonicalTranscript(tr)
	if err != nil {
		return false, fmt.Errorf("marshaling transcript: %w", err)
	}
	sum := sha256.Sum256(canonical)
	freshSHA := hex.EncodeToString(sum[:])

	if !force && !archive.ShouldRewriteTranscript(meta, freshSHA) {
		return false, nil
	}

	if err := archive.WriteAtomic(filepath.Join(folder, "transcript.json"), canonical); err != nil {
		return false, fmt.Errorf("writing transcript.json: %w", err)
	}
	for _, fmtName := range formats {
		if fmtName == "json" {
			continue
		}
		body, rerr := archive.Render(tr, fmtName)
		if rerr != nil {
			return false, fmt.Errorf("rendering transcript.%s: %w", fmtName, rerr)
		}
		if err := archive.WriteAtomic(filepath.Join(folder, "transcript."+fmtName), body); err != nil {
			return false, fmt.Errorf("writing transcript.%s: %w", fmtName, err)
		}
	}

	meta.SetTranscript(archive.MetaTranscript{
		Filename:     "transcript.json",
		SHA256:       freshSHA,
		SegmentCount: len(tr.Segments),
		Language:     detail.Language,
	})
	return true, nil
}

func convertSegments(in []api.Segment) []archive.Segment {
	if in == nil {
		return nil
	}
	out := make([]archive.Segment, len(in))
	for i, s := range in {
		out[i] = archive.Segment{
			Speaker:         s.Speaker,
			OriginalSpeaker: s.OriginalSpeaker,
			StartMs:         s.StartMs,
			EndMs:           s.EndMs,
			Text:            s.Text,
		}
	}
	return out
}

func marshalCanonicalTranscript(tr archive.Transcript) ([]byte, error) {
	raw, err := json.Marshal(tr)
	if err != nil {
		return nil, err
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(generic, "", "  ")
	if err != nil {
		return nil, err
	}
	out = append(out, '\n')
	return out, nil
}

func writeSummary(folder, summary string, meta *archive.Metadata, force bool) (bool, error) {
	body := []byte(summary)
	sum := sha256.Sum256(body)
	freshSHA := hex.EncodeToString(sum[:])

	if !force && !archive.ShouldRewriteSummary(meta, freshSHA) {
		return false, nil
	}

	if err := archive.WriteAtomic(filepath.Join(folder, "summary.plaud.md"), body); err != nil {
		return false, fmt.Errorf("writing summary.plaud.md: %w", err)
	}

	meta.SetSummary(archive.MetaSummary{
		Filename: "summary.plaud.md",
		SHA256:   freshSHA,
	})
	return true, nil
}
