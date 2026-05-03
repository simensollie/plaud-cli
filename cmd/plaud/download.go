package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/simensollie/plaud-cli/internal/api"
	"github.com/simensollie/plaud-cli/internal/archive"
	"github.com/simensollie/plaud-cli/internal/auth"
)

// downloadCmdOpts carries the test seams the download command exposes.
// Tests inject a custom resolver to point at httptest servers and a custom
// audio HTTP client to swap out S3.
type downloadCmdOpts struct {
	resolveBaseURL func(api.Region) (string, error)
	now            func() time.Time
	lookPath       func(string) (string, error)
}

type downloadOption func(*downloadCmdOpts)

// withDownloadBaseURLResolver overrides the region-to-URL resolver. Test seam.
func withDownloadBaseURLResolver(f func(api.Region) (string, error)) downloadOption {
	return func(o *downloadCmdOpts) { o.resolveBaseURL = f }
}

// withDownloadNow overrides the wall-clock used for fetched_at /
// last_verified_at stamps. Test seam.
func withDownloadNow(f func() time.Time) downloadOption {
	return func(o *downloadCmdOpts) { o.now = f }
}

// withDownloadLookPath overrides exec.LookPath. Test seam for F-11.
func withDownloadLookPath(f func(string) (string, error)) downloadOption {
	return func(o *downloadCmdOpts) { o.lookPath = f }
}

func newDownloadCmd(opts ...downloadOption) *cobra.Command {
	o := &downloadCmdOpts{
		resolveBaseURL: api.BaseURL,
		now:            func() time.Time { return time.Now().UTC() },
		lookPath:       exec.LookPath,
	}
	for _, opt := range opts {
		opt(o)
	}

	var (
		outFlag             string
		includeFlag         string
		excludeFlag         string
		transcriptFmtFlag   string
		audioFmtFlag        string
		concurrencyFlag     int
		forceFlag           bool
		formatFlag          string
		includeFlagChanged  bool
		excludeFlagChanged  bool
		transcriptFmtChange bool
		audioFmtChanged     bool
	)

	cmd := &cobra.Command{
		Use:           "download <id> [<id>...]",
		Short:         "Download recordings (transcripts, summaries, audio) into a local archive",
		SilenceUsage:  true,
		SilenceErrors: true,
		Long: `Download one or more recordings into a structured local archive folder.

By default the archive root is ~/PlaudArchive (or %USERPROFILE%\PlaudArchive
on Windows); override with --out DIR or PLAUD_ARCHIVE_DIR.

Default include set: transcript, summary, metadata. Audio is opt-in via
--include audio because audio bytes dominate disk usage.

With --format json, one JSON object per recording is emitted on stdout when
that recording finishes. Shape (not stability-committed before v1.0):
  {"id": "<32-hex>", "status": "fetched"|"skipped"|"failed",
   "files": ["audio.mp3", ...], "duration_ms": 2341, "error": "..."}
The 'error' key is present only when status is "failed". Stderr remains
plain English regardless of --format.

Requires a prior 'plaud login'.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fl := cmd.Flags()
			includeFlagChanged = fl.Changed("include")
			excludeFlagChanged = fl.Changed("exclude")
			transcriptFmtChange = fl.Changed("transcript-format")
			audioFmtChanged = fl.Changed("audio-format")
			return runDownload(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), o, downloadInput{
				ids:               args,
				out:               outFlag,
				include:           includeFlag,
				includeSet:        includeFlagChanged,
				exclude:           excludeFlag,
				excludeSet:        excludeFlagChanged,
				transcriptFormat:  transcriptFmtFlag,
				transcriptFmtSet:  transcriptFmtChange,
				audioFormat:       audioFmtFlag,
				audioFormatSet:    audioFmtChanged,
				concurrency:       concurrencyFlag,
				concurrencyChange: fl.Changed("concurrency"),
				force:             forceFlag,
				format:            formatFlag,
			})
		},
	}

	cmd.Flags().StringVar(&outFlag, "out", "", "override archive root (default: ~/PlaudArchive)")
	cmd.Flags().StringVar(&includeFlag, "include", "", "comma-separated artifact set (audio,transcript,summary,metadata)")
	cmd.Flags().StringVar(&excludeFlag, "exclude", "", "comma-separated artifacts to subtract from the full set")
	cmd.Flags().StringVar(&transcriptFmtFlag, "transcript-format", "", "transcript formats (json,md,srt,vtt,txt). Replaces default json,md")
	cmd.Flags().StringVar(&audioFmtFlag, "audio-format", "mp3", "audio format (mp3 default; other formats require ffmpeg)")
	cmd.Flags().IntVar(&concurrencyFlag, "concurrency", 4, "number of recordings fetched in parallel (1..16)")
	cmd.Flags().BoolVar(&forceFlag, "force", false, "re-fetch every artifact in the include set, bypassing idempotency")
	cmd.Flags().StringVar(&formatFlag, "format", "", "output format: blank for human-readable, 'json' for one JSON line per recording")
	return cmd
}

type downloadInput struct {
	ids               []string
	out               string
	include           string
	includeSet        bool
	exclude           string
	excludeSet        bool
	transcriptFormat  string
	transcriptFmtSet  bool
	audioFormat       string
	audioFormatSet    bool
	concurrency       int
	concurrencyChange bool
	force             bool
	format            string
}

// runDownload is the entry point for the download command. It loads
// credentials, resolves the include set and other flags, walks IDs through
// the worker pool, and prints per-recording results to stdout/stderr.
func runDownload(ctx context.Context, stdout, stderr io.Writer, o *downloadCmdOpts, in downloadInput) error {
	creds, err := auth.Load()
	if errors.Is(err, auth.ErrNotLoggedIn) {
		fmt.Fprintln(stderr, "Not logged in. Run `plaud login` first.")
		return errors.New("not logged in")
	}
	if err != nil {
		return fmt.Errorf("loading credentials: %w", err)
	}

	include, err := resolveIncludeSet(in)
	if err != nil {
		return err
	}

	transcriptFormats, err := resolveTranscriptFormats(in, include)
	if err != nil {
		return err
	}

	if in.audioFormatSet && !include.Audio {
		return fmt.Errorf("--audio-format requires audio in --include; current include set is %s", formatIncludeSet(include))
	}

	concurrency, err := resolveConcurrency(in, stderr)
	if err != nil {
		return err
	}

	audioFormat := strings.TrimSpace(in.audioFormat)
	if audioFormat == "" {
		audioFormat = "mp3"
	}
	if include.Audio && audioFormat != "mp3" {
		if _, err := o.lookPath("ffmpeg"); err != nil {
			fmt.Fprintf(stderr, "ffmpeg not found on PATH; --audio-format %s requires ffmpeg. Falling back to mp3.\n", audioFormat)
			audioFormat = "mp3"
		}
	}

	root, err := resolveArchiveRoot(in.out, stderr)
	if err != nil {
		return err
	}
	if err := archive.ProbeWritable(root); err != nil {
		return fmt.Errorf("archive root not writable: %w", err)
	}

	region := api.Region(creds.Region)
	baseURL, err := o.resolveBaseURL(region)
	if err != nil {
		return fmt.Errorf("resolving region %q: %w", creds.Region, err)
	}
	client, err := api.New(region, creds.Token, api.WithBaseURL(baseURL))
	if err != nil {
		return fmt.Errorf("constructing API client: %w", err)
	}

	resolved, err := resolveIDs(ctx, client, in.ids, stderr)
	if err != nil {
		return err
	}

	jsonMode := in.format == "json"
	var stdoutMu sync.Mutex
	onComplete := func(res recordingResult) {
		if !jsonMode {
			return
		}
		line, mErr := marshalJSONResult(res)
		if mErr != nil {
			return
		}
		stdoutMu.Lock()
		_, _ = stdout.Write(line)
		_, _ = stdout.Write([]byte("\n"))
		stdoutMu.Unlock()
	}

	results := runWorkerPool(ctx, client, root, resolved, include, transcriptFormats, audioFormat, in.force, concurrency, o.now, onComplete)

	emitResults(stdout, stderr, results, jsonMode)

	for _, r := range results {
		if r.status == statusFailed {
			return errors.New("one or more recordings failed")
		}
	}
	return nil
}

// resolveIncludeSet picks the effective include set per the precedence in
// F-04: --include > --exclude > PLAUD_DEFAULT_INCLUDE > built-in default
// (transcript, summary, metadata).
func resolveIncludeSet(in downloadInput) (archive.IncludeSet, error) {
	if in.includeSet && in.excludeSet {
		return archive.IncludeSet{}, errors.New("--include and --exclude are mutually exclusive")
	}
	if in.includeSet {
		return parseIncludeMembers(in.include)
	}
	if in.excludeSet {
		full := archive.IncludeSet{Audio: true, Transcript: true, Summary: true, Metadata: true}
		excluded, err := parseIncludeMembers(in.exclude)
		if err != nil {
			return archive.IncludeSet{}, err
		}
		return subtract(full, excluded), nil
	}
	if env := strings.TrimSpace(os.Getenv("PLAUD_DEFAULT_INCLUDE")); env != "" {
		return parseIncludeMembers(env)
	}
	return archive.IncludeSet{Audio: false, Transcript: true, Summary: true, Metadata: true}, nil
}

func parseIncludeMembers(raw string) (archive.IncludeSet, error) {
	var s archive.IncludeSet
	for _, part := range strings.Split(raw, ",") {
		v := strings.TrimSpace(strings.ToLower(part))
		if v == "" {
			continue
		}
		switch v {
		case "audio":
			s.Audio = true
		case "transcript":
			s.Transcript = true
		case "summary":
			s.Summary = true
		case "metadata":
			s.Metadata = true
		default:
			return archive.IncludeSet{}, fmt.Errorf("unknown include member %q (allowed: audio, transcript, summary, metadata)", v)
		}
	}
	return s, nil
}

func subtract(a, b archive.IncludeSet) archive.IncludeSet {
	return archive.IncludeSet{
		Audio:      a.Audio && !b.Audio,
		Transcript: a.Transcript && !b.Transcript,
		Summary:    a.Summary && !b.Summary,
		Metadata:   a.Metadata && !b.Metadata,
	}
}

func formatIncludeSet(s archive.IncludeSet) string {
	var members []string
	if s.Audio {
		members = append(members, "audio")
	}
	if s.Transcript {
		members = append(members, "transcript")
	}
	if s.Summary {
		members = append(members, "summary")
	}
	if s.Metadata {
		members = append(members, "metadata")
	}
	if len(members) == 0 {
		return "{}"
	}
	return "{" + strings.Join(members, ",") + "}"
}

var allTranscriptFormats = map[string]struct{}{
	"json": {}, "md": {}, "srt": {}, "vtt": {}, "txt": {},
}

func resolveTranscriptFormats(in downloadInput, include archive.IncludeSet) ([]string, error) {
	if in.transcriptFmtSet {
		if !include.Transcript {
			return nil, fmt.Errorf("--transcript-format requires transcript in --include; current include set is %s", formatIncludeSet(include))
		}
		return parseTranscriptFormats(in.transcriptFormat)
	}
	if env := strings.TrimSpace(os.Getenv("PLAUD_DEFAULT_TRANSCRIPT_FORMAT")); env != "" {
		return parseTranscriptFormats(env)
	}
	return []string{"json", "md"}, nil
}

func parseTranscriptFormats(raw string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		v := strings.TrimSpace(strings.ToLower(part))
		if v == "" {
			continue
		}
		if _, ok := allTranscriptFormats[v]; !ok {
			return nil, fmt.Errorf("unknown transcript format %q (allowed: json, md, srt, vtt, txt)", v)
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, errors.New("--transcript-format requires at least one format")
	}
	return out, nil
}

func resolveConcurrency(in downloadInput, stderr io.Writer) (int, error) {
	n := in.concurrency
	if n < 1 {
		return 0, fmt.Errorf("--concurrency must be in [1, 16], got %d", n)
	}
	if n > 16 {
		fmt.Fprintf(stderr, "concurrency %d exceeds maximum 16; clamping to 16\n", n)
		n = 16
	}
	return n, nil
}

func resolveArchiveRoot(outFlag string, stderr io.Writer) (string, error) {
	root := strings.TrimSpace(outFlag)
	usingDefault := false
	if root == "" {
		if env := strings.TrimSpace(os.Getenv("PLAUD_ARCHIVE_DIR")); env != "" {
			root = env
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("resolving home directory: %w", err)
			}
			root = filepath.Join(home, "PlaudArchive")
			usingDefault = true
		}
	}
	created, err := archive.EnsureRoot(root)
	if err != nil {
		return "", fmt.Errorf("preparing archive root: %w", err)
	}
	if created && usingDefault {
		fmt.Fprintf(stderr, "Created archive root at %s\n", root)
	}
	return root, nil
}

var hexIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// resolveIDs walks each positional arg: hex IDs pass through; non-hex args
// trigger one client.List call (cached for the run) and prefix-match against
// Filename. Trashed recordings are not surfaced via prefix because the list
// endpoint already filters them out.
func resolveIDs(ctx context.Context, client *api.Client, args []string, stderr io.Writer) ([]api.Recording, error) {
	hasNonHex := false
	for _, a := range args {
		if !hexIDPattern.MatchString(a) {
			hasNonHex = true
			break
		}
	}

	var listing []api.Recording
	if hasNonHex {
		recs, err := client.List(ctx)
		if errors.Is(err, api.ErrUnauthorized) {
			fmt.Fprintln(stderr, "Token expired or invalid. Run `plaud login` again.")
			return nil, errors.New("unauthorized")
		}
		if err != nil {
			return nil, fmt.Errorf("listing recordings: %w", err)
		}
		listing = recs
	}

	out := make([]api.Recording, 0, len(args))
	for _, arg := range args {
		if hexIDPattern.MatchString(arg) {
			out = append(out, api.Recording{ID: arg})
			continue
		}
		matches := matchByPrefix(listing, arg)
		switch len(matches) {
		case 0:
			return nil, fmt.Errorf("no recording matched %q", arg)
		case 1:
			out = append(out, matches[0])
		default:
			return nil, fmt.Errorf("ambiguous prefix %q matched %d recordings:\n%s",
				arg, len(matches), candidateLines(matches))
		}
	}
	return out, nil
}

func matchByPrefix(recs []api.Recording, prefix string) []api.Recording {
	lower := strings.ToLower(prefix)
	var out []api.Recording
	for _, r := range recs {
		if strings.HasPrefix(strings.ToLower(r.Filename), lower) {
			out = append(out, r)
		}
	}
	return out
}

func candidateLines(recs []api.Recording) string {
	var b strings.Builder
	for _, r := range recs {
		fmt.Fprintf(&b, "  %s  %s\n", r.ID, r.Filename)
	}
	return strings.TrimRight(b.String(), "\n")
}

type recordingStatus string

const (
	statusFetched recordingStatus = "fetched"
	statusSkipped recordingStatus = "skipped"
	statusFailed  recordingStatus = "failed"
)

type recordingResult struct {
	id           string
	status       recordingStatus
	files        []string
	skippedFiles []string
	durationMs   int64
	err          error
}

// jsonResult is the wire shape of one --format json object on stdout.
// Documented in --help; not stability-committed before v1.0 (F-12).
type jsonResult struct {
	ID         string   `json:"id"`
	Status     string   `json:"status"`
	Files      []string `json:"files"`
	DurationMs int64    `json:"duration_ms"`
	Error      string   `json:"error,omitempty"`
}

// runWorkerPool dispatches one goroutine per recording, capped at
// concurrency. A 401 from any worker cancels the parent context so the
// other workers' in-flight HTTP calls abort. Queued recordings that have
// not started are skipped.
func runWorkerPool(
	parentCtx context.Context,
	client *api.Client,
	root string,
	recs []api.Recording,
	include archive.IncludeSet,
	transcriptFormats []string,
	audioFormat string,
	force bool,
	concurrency int,
	now func() time.Time,
	onComplete func(recordingResult),
) []recordingResult {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	results := make([]recordingResult, len(recs))
	type job struct {
		idx int
		rec api.Recording
	}
	jobs := make(chan job)
	var wg sync.WaitGroup

	var (
		authOnce  sync.Once
		authMsg   string
		authMutex sync.Mutex
	)
	flagAuth := func(msg string) {
		authOnce.Do(func() {
			authMutex.Lock()
			authMsg = msg
			authMutex.Unlock()
			cancel()
		})
	}

	emit := func(res recordingResult) {
		if onComplete != nil {
			onComplete(res)
		}
	}

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					res := recordingResult{
						id:     j.rec.ID,
						status: statusFailed,
						err:    errors.New("cancelled"),
					}
					results[j.idx] = res
					emit(res)
					continue
				}
				res := processRecording(ctx, client, root, j.rec, include, transcriptFormats, audioFormat, force, now)
				if res.err != nil && errors.Is(res.err, api.ErrUnauthorized) {
					flagAuth("Token expired or invalid. Run `plaud login` again.")
				}
				results[j.idx] = res
				emit(res)
			}
		}()
	}

	for i, r := range recs {
		select {
		case <-ctx.Done():
			res := recordingResult{
				id:     r.ID,
				status: statusFailed,
				err:    errors.New("cancelled"),
			}
			results[i] = res
			emit(res)
		case jobs <- job{idx: i, rec: r}:
		}
	}
	close(jobs)
	wg.Wait()

	authMutex.Lock()
	msg := authMsg
	authMutex.Unlock()
	if msg != "" {
		for i, res := range results {
			if res.status == statusFailed && res.err != nil && errors.Is(res.err, api.ErrUnauthorized) {
				results[i].err = errors.New(msg)
			}
		}
	}
	return results
}

// processRecording is the per-recording orchestration: trash warning,
// partial sweep, metadata load/rebuild, detail call, audio fetch, transcript
// rendering, summary, and metadata write.
func processRecording(
	ctx context.Context,
	client *api.Client,
	root string,
	rec api.Recording,
	include archive.IncludeSet,
	transcriptFormats []string,
	audioFormat string,
	force bool,
	now func() time.Time,
) recordingResult {
	start := now()
	res := recordingResult{id: rec.ID}

	// Phase 5 always fetches detail. /file/detail carries is_trash, start_time,
	// duration, and language alongside the artifact pointers; we need it both
	// for direct-ID resolution (no preceding list call) and for the F-17
	// trash warning.
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

	slug := archive.Slug(rec.Filename)
	if slug == "untitled" {
		slug = archive.SlugWithCollision(rec.Filename, rec.ID, func(s string) bool { return s == "untitled" })
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

	if rec.IsTrash {
		// The caller (caller's caller, really) prints this on stderr; we
		// stash it on the result so emitResults can decide on the format.
		res.files = append(res.files, "(trashed)")
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
		res.files = append(res.files, "(metadata-rebuilt)")
	}

	anyWrite := false
	skippedAll := true

	if include.Audio {
		written, skipped, audioErr := fetchAudio(ctx, client, folder, audioFormat, rec, meta, force)
		if audioErr != nil {
			return failResult(res, audioErr, start, now)
		}
		audioName := "audio." + audioFormat
		if written {
			anyWrite = true
			res.files = append(res.files, audioName)
		} else if skipped {
			res.skippedFiles = append(res.skippedFiles, audioName)
		}
		if !skipped {
			skippedAll = false
		}
	}

	if include.Transcript {
		switch {
		case detail == nil || detail.Segments == nil:
			res.files = append(res.files, "(transcript-not-ready)")
		default:
			written, transErr := writeTranscript(folder, detail, transcriptFormats, meta, force)
			if transErr != nil {
				return failResult(res, transErr, start, now)
			}
			if written {
				anyWrite = true
				skippedAll = false
				res.files = append(res.files, "transcript.json")
				for _, fmtName := range transcriptFormats {
					if fmtName == "json" {
						continue
					}
					res.files = append(res.files, "transcript."+fmtName)
				}
			} else {
				res.skippedFiles = append(res.skippedFiles, "transcript.json")
				for _, fmtName := range transcriptFormats {
					if fmtName == "json" {
						continue
					}
					res.skippedFiles = append(res.skippedFiles, "transcript."+fmtName)
				}
			}
		}
	}

	if include.Summary {
		switch {
		case detail == nil || strings.TrimSpace(detail.Summary) == "":
			res.files = append(res.files, "(summary-not-ready)")
		default:
			written, sumErr := writeSummary(folder, detail.Summary, meta, force)
			if sumErr != nil {
				return failResult(res, sumErr, start, now)
			}
			if written {
				anyWrite = true
				skippedAll = false
				res.files = append(res.files, "summary.plaud.md")
			} else {
				res.skippedFiles = append(res.skippedFiles, "summary.plaud.md")
			}
		}
	}

	// Metadata is always written if any other artifact landed (F-07(e), §4).
	// On no-op runs we still bump last_verified_at and rewrite.
	if force {
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
	res.files = append(res.files, archive.MetadataFilename)

	res.durationMs = now().Sub(start).Milliseconds()
	if force {
		res.status = statusFetched
	} else if !anyWrite && skippedAll {
		res.status = statusSkipped
	} else {
		res.status = statusFetched
	}
	return res
}

func failResult(res recordingResult, err error, start time.Time, now func() time.Time) recordingResult {
	res.status = statusFailed
	res.err = err
	res.durationMs = now().Sub(start).Milliseconds()
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

// fetchAudio handles the two-step signed-URL flow with one retry on a
// 401/403 from S3 (signature expiry). Returns (written, skipped, err):
// written=true when audio bytes hit disk this run; skipped=true when the
// HEAD ETag matched and no GET was issued.
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

	head, err := client.HeadAudio(ctx, signedURL)
	if errors.Is(err, api.ErrSignedURLExpired) {
		signedURL, err = client.TempURL(ctx, rec.ID)
		if err != nil {
			return false, false, fmt.Errorf("refetching audio URL: %w", err)
		}
		head, err = client.HeadAudio(ctx, signedURL)
		if err != nil {
			return false, false, fmt.Errorf("HEAD audio after retry: %w", err)
		}
	} else if err != nil {
		return false, false, fmt.Errorf("HEAD audio: %w", err)
	}

	if !force && meta.Audio != nil && head.ETag != "" && head.ETag == meta.Audio.S3ETag {
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

// marshalCanonicalTranscript renders the transcript as pretty-printed JSON
// with sorted keys and a trailing newline. Matches the metadata.json
// formatting so transcript SHA-256 is stable across rewrites.
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

// emitResults prints per-recording status. Stderr warnings always fire
// (plain English regardless of --format). Human-readable stdout fires only
// when jsonMode is false; under --format json the per-recording JSON line
// is already emitted at completion time by runDownload's onComplete hook.
func emitResults(stdout, stderr io.Writer, results []recordingResult, jsonMode bool) {
	for _, r := range results {
		if r.status == statusFailed {
			fmt.Fprintf(stderr, "%s: %s\n", r.id, redactErrorString(r.err))
			continue
		}
		if !jsonMode {
			fmt.Fprintf(stdout, "%s\t%s\t%dms\n", r.id, r.status, r.durationMs)
		}
		for _, f := range r.files {
			switch f {
			case "(trashed)":
				fmt.Fprintf(stderr, "%s: recording is trashed; downloading anyway\n", r.id)
			case "(transcript-not-ready)":
				fmt.Fprintf(stderr, "%s: transcript not yet ready, skipped\n", r.id)
			case "(summary-not-ready)":
				fmt.Fprintf(stderr, "%s: summary not yet ready, skipped\n", r.id)
			case "(metadata-rebuilt)":
				fmt.Fprintf(stderr, "%s: metadata.json was unparseable, rebuilt from local files\n", r.id)
			}
		}
	}
}

// marshalJSONResult renders one per-recording result as a single-line JSON
// object suitable for --format json output. Files are alphabetically sorted
// per F-12. Sentinel markers (e.g. "(trashed)") are stripped.
func marshalJSONResult(r recordingResult) ([]byte, error) {
	out := jsonResult{
		ID:         r.id,
		Status:     string(r.status),
		DurationMs: r.durationMs,
	}
	switch r.status {
	case statusFetched:
		out.Files = filterAndSortFiles(r.files)
	case statusSkipped:
		merged := append([]string{}, r.skippedFiles...)
		merged = append(merged, r.files...)
		out.Files = filterAndSortFiles(merged)
	case statusFailed:
		out.Files = []string{}
		if r.err != nil {
			out.Error = redactErrorString(r.err)
		}
	}
	if out.Files == nil {
		out.Files = []string{}
	}
	return json.Marshal(out)
}

func filterAndSortFiles(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, f := range in {
		if strings.HasPrefix(f, "(") && strings.HasSuffix(f, ")") {
			continue
		}
		if seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	// Manual insertion sort keeps the dependency surface small and the slice
	// length is bounded by the include set (single-digit entries).
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j-1] > out[j] {
			out[j-1], out[j] = out[j], out[j-1]
			j--
		}
	}
	return out
}

var signedURLPattern = regexp.MustCompile(`https?://[^\s"]+`)

// redactErrorString returns err.Error() with HTTP(S) URLs and any
// Authorization-style header tokens stripped, per F-13. Errors carrying
// signed CDN URLs or bearer tokens must never reach stdout/stderr.
func redactErrorString(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	s = signedURLPattern.ReplaceAllString(s, "<redacted-url>")
	return s
}
