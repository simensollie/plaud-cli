package main

import (
	"context"
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
	"github.com/simensollie/plaud-cli/internal/fetch"
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
	onComplete := func(res fetch.Result) {
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

	results := runWorkerPool(ctx, client, root, resolved, fetch.Options{
		Include:           include,
		TranscriptFormats: transcriptFormats,
		AudioFormat:       audioFormat,
		Force:             in.force,
	}, concurrency, o.now, onComplete)

	emitResults(stdout, stderr, results, jsonMode)

	for _, r := range results {
		if r.Status == fetch.StatusFailed {
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
	opts fetch.Options,
	concurrency int,
	now func() time.Time,
	onComplete func(fetch.Result),
) []fetch.Result {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	results := make([]fetch.Result, len(recs))
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

	emit := func(res fetch.Result) {
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
					res := fetch.Result{
						ID:     j.rec.ID,
						Status: fetch.StatusFailed,
						Err:    errors.New("cancelled"),
					}
					results[j.idx] = res
					emit(res)
					continue
				}
				res := fetch.FetchOne(ctx, client, root, j.rec, opts, now)
				if res.Err != nil && errors.Is(res.Err, api.ErrUnauthorized) {
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
			res := fetch.Result{
				ID:     r.ID,
				Status: fetch.StatusFailed,
				Err:    errors.New("cancelled"),
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
			if res.Status == fetch.StatusFailed && res.Err != nil && errors.Is(res.Err, api.ErrUnauthorized) {
				results[i].Err = errors.New(msg)
			}
		}
	}
	return results
}

// emitResults prints per-recording status. Stderr warnings always fire
// (plain English regardless of --format). Human-readable stdout fires only
// when jsonMode is false; under --format json the per-recording JSON line
// is already emitted at completion time by runDownload's onComplete hook.
func emitResults(stdout, stderr io.Writer, results []fetch.Result, jsonMode bool) {
	for _, r := range results {
		if r.Status == fetch.StatusFailed {
			fmt.Fprintf(stderr, "%s: %s\n", r.ID, redactErrorString(r.Err))
			continue
		}
		if !jsonMode {
			fmt.Fprintf(stdout, "%s\t%s\t%dms\n", r.ID, r.Status, r.DurationMs)
		}
		for _, f := range r.Files {
			switch f {
			case "(trashed)":
				fmt.Fprintf(stderr, "%s: recording is trashed; downloading anyway\n", r.ID)
			case "(transcript-not-ready)":
				fmt.Fprintf(stderr, "%s: transcript not yet ready, skipped\n", r.ID)
			case "(summary-not-ready)":
				fmt.Fprintf(stderr, "%s: summary not yet ready, skipped\n", r.ID)
			case "(metadata-rebuilt)":
				fmt.Fprintf(stderr, "%s: metadata.json was unparseable, rebuilt from local files\n", r.ID)
			}
		}
	}
}

// marshalJSONResult renders one per-recording result as a single-line JSON
// object suitable for --format json output. Files are alphabetically sorted
// per F-12. Sentinel markers (e.g. "(trashed)") are stripped.
func marshalJSONResult(r fetch.Result) ([]byte, error) {
	out := jsonResult{
		ID:         r.ID,
		Status:     string(r.Status),
		DurationMs: r.DurationMs,
	}
	switch r.Status {
	case fetch.StatusFetched:
		out.Files = filterAndSortFiles(r.Files)
	case fetch.StatusSkipped:
		merged := append([]string{}, r.Skipped...)
		merged = append(merged, r.Files...)
		out.Files = filterAndSortFiles(merged)
	case fetch.StatusFailed:
		out.Files = []string{}
		if r.Err != nil {
			out.Error = redactErrorString(r.Err)
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

// redactErrorString returns err.Error() with credential-bearing patterns
// stripped, per F-13. Delegates to api.RedactError so the same patterns
// apply to spec 0003 sync's surfaces.
func redactErrorString(err error) string {
	return api.RedactError(err)
}
