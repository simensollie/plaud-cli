package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/simensollie/plaud-cli/internal/api"
	"github.com/simensollie/plaud-cli/internal/archive"
	"github.com/simensollie/plaud-cli/internal/auth"
	syncpkg "github.com/simensollie/plaud-cli/internal/sync"
)

// syncCmdOpts carries the test seams the sync command exposes.
type syncCmdOpts struct {
	resolveBaseURL func(api.Region) (string, error)
	now            func() time.Time
	sleep          func(d time.Duration) <-chan time.Time
}

type syncOption func(*syncCmdOpts)

func withSyncBaseURLResolver(f func(api.Region) (string, error)) syncOption {
	return func(o *syncCmdOpts) { o.resolveBaseURL = f }
}

func withSyncNow(f func() time.Time) syncOption {
	return func(o *syncCmdOpts) { o.now = f }
}

// withSyncSleep overrides the inter-cycle sleep primitive for watch mode.
// Test seam: tests inject an instant sleep so cycles fly by.
func withSyncSleep(f func(d time.Duration) <-chan time.Time) syncOption {
	return func(o *syncCmdOpts) { o.sleep = f }
}

func newSyncCmd(opts ...syncOption) *cobra.Command {
	o := &syncCmdOpts{
		resolveBaseURL: api.BaseURL,
		now:            func() time.Time { return time.Now().UTC() },
		sleep:          func(d time.Duration) <-chan time.Time { return time.After(d) },
	}
	for _, opt := range opts {
		opt(o)
	}

	var (
		outFlag         string
		watchFlag       bool
		intervalFlag    time.Duration
		concurrencyFlag int
		includeFlag     string
		pruneFlag       bool
		pruneEmptyFlag  bool
		includeTrashed  bool
		formatFlag      string
		dryRunFlag      bool
		forceFlag       bool
	)

	cmd := &cobra.Command{
		Use:           "sync",
		Short:         "Mirror every recording from your Plaud account into a local archive",
		SilenceUsage:  true,
		SilenceErrors: true,
		Long: `Bring the local archive up to date with every recording on the account.

By default, sync fetches transcripts, summaries, and metadata only (text-only
archive per F-14). Pass --include audio,transcript,summary,metadata to mirror
audio bytes too, or set PLAUD_DEFAULT_INCLUDE.

A single instance writes to a given archive root at a time (F-11). Run
plaud sync once a day from cron / launchd / systemd / Task Scheduler; the
--watch loop is for desk sessions, not unattended uptime.

With --format json, an NDJSON event stream is emitted on stdout. Schema is
preview in 0.3.0-rc and field-level stable from 0.3.0 GA (F-07).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fl := cmd.Flags()
			return runSync(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), o, syncInput{
				out:               outFlag,
				watch:             watchFlag,
				interval:          intervalFlag,
				concurrency:       concurrencyFlag,
				concurrencyChange: fl.Changed("concurrency"),
				include:           includeFlag,
				includeSet:        fl.Changed("include"),
				prune:             pruneFlag,
				pruneEmpty:        pruneEmptyFlag,
				includeTrashed:    includeTrashed,
				format:            formatFlag,
				dryRun:            dryRunFlag,
				force:             forceFlag,
			})
		},
	}

	cmd.Flags().StringVar(&outFlag, "out", "", "override archive root (default: ~/PlaudArchive)")
	cmd.Flags().BoolVar(&watchFlag, "watch", false, "loop: poll, sync new, sleep")
	cmd.Flags().DurationVar(&intervalFlag, "interval", 15*time.Minute, "sleep duration between sync cycles in --watch mode")
	cmd.Flags().IntVar(&concurrencyFlag, "concurrency", 4, "number of recordings fetched in parallel (1..16)")
	cmd.Flags().StringVar(&includeFlag, "include", "", "comma-separated artifact set (audio,transcript,summary,metadata); default text-only")
	cmd.Flags().BoolVar(&pruneFlag, "prune", false, "move recordings absent from the server to .trash/ (default: never)")
	cmd.Flags().BoolVar(&pruneEmptyFlag, "prune-empty", false, "bypass mass-deletion guards (only meaningful with --prune)")
	cmd.Flags().BoolVar(&includeTrashed, "include-trashed", false, "also fetch recordings with is_trash=true")
	cmd.Flags().StringVar(&formatFlag, "format", "", "output format: blank for human-readable, 'json' for NDJSON event stream")
	cmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "report what would be fetched / skipped / pruned without making changes")
	cmd.Flags().BoolVar(&forceFlag, "force", false, "re-fetch every artifact in the include set, bypassing idempotency")
	return cmd
}

type syncInput struct {
	out               string
	watch             bool
	interval          time.Duration
	concurrency       int
	concurrencyChange bool
	include           string
	includeSet        bool
	prune             bool
	pruneEmpty        bool
	includeTrashed    bool
	format            string
	dryRun            bool
	force             bool
}

func runSync(ctx context.Context, stdout, stderr io.Writer, o *syncCmdOpts, in syncInput) error {
	creds, err := auth.Load()
	if errors.Is(err, auth.ErrNotLoggedIn) {
		fmt.Fprintln(stderr, "Not logged in. Run `plaud login` first.")
		return errors.New("not logged in")
	}
	if err != nil {
		return fmt.Errorf("loading credentials: %w", err)
	}

	include, err := resolveSyncInclude(in)
	if err != nil {
		return err
	}

	concurrency, err := resolveSyncConcurrency(in, stderr)
	if err != nil {
		return err
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
	client, err := api.New(region, creds.Token, api.WithBaseURL(baseURL), api.WithBackoffTransport())
	if err != nil {
		return fmt.Errorf("constructing API client: %w", err)
	}

	jsonMode := in.format == "json"
	emitter := &syncpkg.EventEmitter{Out: stdout, Now: o.now}

	if in.watch {
		return runWatch(ctx, stdout, stderr, o, client, root, in, include, concurrency, jsonMode, emitter)
	}
	_, err = runSyncOnce(ctx, stdout, stderr, o, client, root, in, include, concurrency, jsonMode, emitter)
	return err
}

// runSyncOnce executes one cycle: lock, list, reconcile, run, unlock.
// Returns the cycle's RunResult so watch mode can decide whether to count
// it as a success or a failure. F-07: a `done` event fires on every exit
// path, with `details.status` reflecting the outcome (ok / interrupted /
// failed) so NDJSON consumers never wait forever.
func runSyncOnce(
	ctx context.Context,
	stdout, stderr io.Writer,
	o *syncCmdOpts,
	client *api.Client,
	root string,
	in syncInput,
	include archive.IncludeSet,
	concurrency int,
	jsonMode bool,
	emitter *syncpkg.EventEmitter,
) (syncpkg.RunResult, error) {
	res := syncpkg.RunResult{Status: "failed"}
	defer func() {
		if !jsonMode {
			return
		}
		details := map[string]any{
			"status":     res.Status,
			"discovered": res.Discovered,
			"fetched":    res.Fetched,
			"skipped":    res.Skipped,
			"verified":   res.Verified,
			"failed":     res.Failed,
			"pruned":     res.Pruned,
		}
		if in.dryRun {
			details["dry_run"] = true
		}
		emitter.Emit(string(syncpkg.EventNameDone), "", details)
	}()

	if err := ctx.Err(); err != nil {
		res.Status = "interrupted"
		return res, err
	}

	lock, holder, err := syncpkg.AcquireLock(root)
	if errors.Is(err, syncpkg.ErrLocked) {
		fmt.Fprintf(stderr, "plaud sync is already running on this archive root (PID %d, host %q, started %s).\n",
			holder.PID, holder.Hostname, holder.StartedAt.Format(time.RFC3339))
		return res, errors.New("locked")
	}
	if err != nil {
		return res, fmt.Errorf("acquiring lock: %w", err)
	}
	defer func() { _ = lock.Release() }()

	state, err := syncpkg.Load(root)
	if err != nil {
		return res, fmt.Errorf("loading state: %w", err)
	}

	listDefault, err := client.List(ctx)
	if errors.Is(err, api.ErrUnauthorized) {
		fmt.Fprintln(stderr, "Token expired or invalid. Run `plaud login` again.")
		return res, errors.New("unauthorized")
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		res.Status = "interrupted"
		return res, err
	}
	if err != nil {
		return res, fmt.Errorf("listing recordings: %w", err)
	}

	var listTrashed []api.Recording
	if in.prune || in.includeTrashed {
		listTrashed, err = client.ListTrashed(ctx)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			res.Status = "interrupted"
			return res, err
		}
		if err != nil {
			return res, fmt.Errorf("listing trashed: %w", err)
		}
	}

	rec, err := syncpkg.Reconcile(listDefault, listTrashed, state, &syncpkg.OSFilesystem{Root: root}, syncpkg.ReconcileOptions{
		Include:        include,
		Prune:          in.prune,
		IncludeTrashed: in.includeTrashed,
		PruneEmpty:     in.pruneEmpty,
	})
	if err != nil {
		// Mass-deletion or similar.
		fmt.Fprintln(stderr, api.RedactError(err))
		return res, err
	}

	if jsonMode {
		emitter.Emit(string(syncpkg.EventNameDiscovered), "", map[string]any{
			"count":         len(rec.Actions),
			"trashed_count": len(listTrashed),
		})
	}

	if in.dryRun {
		runDryRun(emitter, rec.Actions, jsonMode, stdout)
		state.LastRunStarted = o.now().UTC()
		if err := syncpkg.Save(root, state); err != nil {
			return res, err
		}
		res.Status = "ok"
		res.Discovered = len(rec.Actions)
		return res, nil
	}

	runner := &syncpkg.Runner{
		Client: client,
		Root:   root,
		State:  state,
		Pruner: &syncpkg.FolderPruner{Now: o.now},
		Now:    o.now,
		Emit:   emitterAdapter(emitter, stdout, stderr, jsonMode),
	}

	res, err = runner.Run(ctx, rec.Actions, syncpkg.RunOptions{
		Concurrency:       concurrency,
		Include:           include,
		TranscriptFormats: []string{"json", "md"},
		AudioFormat:       "mp3",
		Force:             in.force,
	})
	if err != nil {
		return res, err
	}
	if !jsonMode {
		fmt.Fprintf(stderr, "Cycle done: %d fetched, %d skipped, %d verified, %d failed, %d pruned.\n",
			res.Fetched, res.Skipped, res.Verified, res.Failed, res.Pruned)
	}

	if res.Failed > 0 {
		return res, errors.New("one or more recordings failed")
	}
	return res, nil
}

// runDryRun emits would-* events for the actions instead of executing.
func runDryRun(emitter *syncpkg.EventEmitter, actions []syncpkg.Action, jsonMode bool, stdout io.Writer) {
	for _, a := range actions {
		switch a.Kind {
		case syncpkg.ActionFetch, syncpkg.ActionRename:
			details := map[string]any{"reason": string(a.Reason), "folder": a.Folder}
			if jsonMode {
				emitter.Emit(string(syncpkg.EventNameWouldFetch), a.Recording.ID, details)
			} else {
				fmt.Fprintf(stdout, "would-fetch %s (%s) %s\n", a.Recording.ID, a.Reason, a.Folder)
			}
		case syncpkg.ActionSkip:
			details := map[string]any{"reason": string(a.Reason), "folder": a.Folder}
			if jsonMode {
				emitter.Emit(string(syncpkg.EventNameWouldSkip), a.Recording.ID, details)
			} else {
				fmt.Fprintf(stdout, "would-skip %s (%s)\n", a.Recording.ID, a.Reason)
			}
		case syncpkg.ActionPrune:
			details := map[string]any{"old_folder": a.OldRelative}
			if jsonMode {
				emitter.Emit(string(syncpkg.EventNameWouldPrune), a.Recording.ID, details)
			} else {
				fmt.Fprintf(stdout, "would-prune %s (%s)\n", a.Recording.ID, a.OldRelative)
			}
		}
	}
}

// emitterAdapter bridges syncpkg.Event into NDJSON envelopes (jsonMode) or
// human-readable stderr lines (otherwise). Surface-level redaction lives
// in the emitter itself; the adapter just chooses the sink.
func emitterAdapter(em *syncpkg.EventEmitter, stdout, stderr io.Writer, jsonMode bool) func(syncpkg.Event) {
	return func(e syncpkg.Event) {
		details := map[string]any{}
		if e.Reason != "" {
			details["reason"] = string(e.Reason)
		}
		if e.Result != nil {
			if e.Result.Folder != "" {
				details["folder"] = e.Result.Folder
			}
			if len(e.Result.Files) > 0 {
				details["artifacts"] = filterFilesForEvent(e.Result.Files)
			}
		}
		if e.Err != nil {
			details["error"] = api.RedactError(e.Err)
			fmt.Fprintf(stderr, "%s: %s\n", e.ID, api.RedactError(e.Err))
		}

		if jsonMode {
			em.Emit(string(e.Kind), e.ID, details)
		}
	}
}

func filterFilesForEvent(files []string) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		if strings.HasPrefix(f, "(") && strings.HasSuffix(f, ")") {
			continue
		}
		out = append(out, f)
	}
	return out
}

// resolveSyncInclude picks the effective include set for sync.
// Precedence: --include flag > PLAUD_DEFAULT_INCLUDE > built-in default
// (transcript,summary,metadata) per F-14.
func resolveSyncInclude(in syncInput) (archive.IncludeSet, error) {
	if in.includeSet {
		return parseIncludeMembers(in.include)
	}
	if env := strings.TrimSpace(os.Getenv("PLAUD_DEFAULT_INCLUDE")); env != "" {
		return parseIncludeMembers(env)
	}
	return archive.IncludeSet{Audio: false, Transcript: true, Summary: true, Metadata: true}, nil
}

// resolveSyncConcurrency clamps per F-06 same as download.
func resolveSyncConcurrency(in syncInput, stderr io.Writer) (int, error) {
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

// maxConsecutiveWatchFailures bounds how many consecutive failed cycles
// the watcher tolerates before exiting non-zero (F-05). Higher values
// would mask sustained outages; lower values would over-trip on transient
// network blips.
const maxConsecutiveWatchFailures = 5

// runWatch loops sync cycles with sleep-duration interval (cycle-end → next
// cycle-start, F-05). Acquires a watch sentinel for two-watch detection
// (F-11). Exits cleanly on context cancellation; exits non-zero after
// maxConsecutiveWatchFailures consecutive failed cycles.
func runWatch(
	ctx context.Context,
	stdout, stderr io.Writer,
	o *syncCmdOpts,
	client *api.Client,
	root string,
	in syncInput,
	include archive.IncludeSet,
	concurrency int,
	jsonMode bool,
	emitter *syncpkg.EventEmitter,
) error {
	sentinel, holder, err := syncpkg.AcquireWatchSentinel(root)
	if errors.Is(err, syncpkg.ErrWatchActive) {
		fmt.Fprintf(stderr,
			"A plaud sync watch loop is already active on this archive root (PID %d, host %q, started %s). Stop it before starting another, or remove .plaud-sync.watch if you're sure it's stale.\n",
			holder.PID, holder.Hostname, holder.StartedAt.Format(time.RFC3339))
		return errors.New("watch already active")
	}
	if err != nil {
		return fmt.Errorf("acquiring watch sentinel: %w", err)
	}
	defer func() { _ = sentinel.Release() }()

	consecutiveFailures := 0
	cycle := 0
	for {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil
		}
		cycle++
		_, runErr := runSyncOnce(ctx, stdout, stderr, o, client, root, in, include, concurrency, jsonMode, emitter)
		if runErr != nil {
			if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
				return nil
			}
			consecutiveFailures++
			if consecutiveFailures >= maxConsecutiveWatchFailures {
				fmt.Fprintf(stderr,
					"Watch loop has failed %d cycles in a row. Last error: %s. Exiting; restart after resolving.\n",
					maxConsecutiveWatchFailures, api.RedactError(runErr))
				return runErr
			}
		} else {
			consecutiveFailures = 0
		}

		select {
		case <-ctx.Done():
			return nil
		case <-o.sleep(in.interval):
		}
	}
}
