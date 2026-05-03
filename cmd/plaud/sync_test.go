package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/simensollie/plaud-cli/internal/api"
)

// runSyncCmd builds the sync command pinned at the fake API and runs it.
func runSyncCmd(t *testing.T, fp *fakePlaudServer, args ...string) (string, string, error) {
	t.Helper()
	now := time.Date(2026, 5, 1, 9, 14, 21, 0, time.UTC)
	cmd := newSyncCmd(
		withSyncBaseURLResolver(func(_ api.Region) (string, error) { return fp.api.URL, nil }),
		withSyncNow(func() time.Time { return now }),
	)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

// ---------------------------------------------------------------------------
// F-01 / F-02: first-run + idempotent re-run
// ---------------------------------------------------------------------------

func TestSync_F01_FirstRunFetchesAll(t *testing.T) {
	setTempConfig(t)
	seedCreds(t)
	root := withTempArchive(t)

	r1 := happyRecording("a3f9c021000000000000000000000001")
	r2 := happyRecording("b3f9c021000000000000000000000002")
	fp := newFakePlaud(t, []fakeRecording{r1, r2})

	stdout, stderr, err := runSyncCmd(t, fp)
	if err != nil {
		t.Fatalf("runSync: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	// State file written.
	if _, err := os.Stat(filepath.Join(root, ".plaud-sync.state")); err != nil {
		t.Errorf(".plaud-sync.state missing: %v", err)
	}
	// Both recordings have folders.
	for _, rec := range []fakeRecording{r1, r2} {
		var found bool
		_ = filepath.Walk(root, func(p string, info os.FileInfo, _ error) error {
			if info != nil && info.Name() == "metadata.json" {
				raw, _ := os.ReadFile(p)
				if bytes.Contains(raw, []byte(rec.id)) {
					found = true
				}
			}
			return nil
		})
		if !found {
			t.Errorf("metadata.json missing for %s", rec.id)
		}
	}
}

func TestSync_F02_SecondRunNoFetches(t *testing.T) {
	setTempConfig(t)
	seedCreds(t)
	withTempArchive(t)

	rec := happyRecording("a3f9c021000000000000000000000001")
	fp := newFakePlaud(t, []fakeRecording{rec})

	if _, _, err := runSyncCmd(t, fp); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// Reset api call counter, then sync again.
	fp.apiCnt.Store(0)
	stdout, stderr, err := runSyncCmd(t, fp, "--format", "json")
	if err != nil {
		t.Fatalf("second sync: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	// Verify NDJSON: at least one skipped event for the recording.
	saw := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if line == "" {
			continue
		}
		var env map[string]any
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			t.Fatalf("malformed NDJSON line: %q", line)
		}
		if e, ok := env["event"].(string); ok {
			saw[e]++
		}
	}
	if saw["skipped"] == 0 {
		t.Errorf("expected at least one skipped event on second run; got %v", saw)
	}
	if saw["fetched"] != 0 {
		t.Errorf("expected 0 fetched events on second run; got %d (%v)", saw["fetched"], saw)
	}
}

// ---------------------------------------------------------------------------
// F-07: NDJSON envelope + done-on-interrupt
// ---------------------------------------------------------------------------

func TestSync_F07_NDJSONEventEnvelope(t *testing.T) {
	setTempConfig(t)
	seedCreds(t)
	withTempArchive(t)

	rec := happyRecording("a3f9c021000000000000000000000001")
	fp := newFakePlaud(t, []fakeRecording{rec})

	stdout, _, err := runSyncCmd(t, fp, "--format", "json")
	if err != nil {
		t.Fatalf("runSync: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if line == "" {
			continue
		}
		var env map[string]any
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			t.Fatalf("malformed NDJSON line: %q", line)
		}
		for _, key := range []string{"schema_version", "event", "ts"} {
			if _, ok := env[key]; !ok {
				t.Errorf("event %q missing %q field", line, key)
			}
		}
		if v, ok := env["schema_version"].(float64); !ok || int(v) != 1 {
			t.Errorf("schema_version=%v, want 1", env["schema_version"])
		}
	}
}

// TestSync_F07_DoneFiresOnSIGINTWithStatusInterrupted asserts that a
// pre-cancelled context still produces a done event with status
// "interrupted" so consumers tailing the stream never wait forever.
func TestSync_F07_DoneFiresOnSIGINTWithStatusInterrupted(t *testing.T) {
	setTempConfig(t)
	seedCreds(t)
	withTempArchive(t)

	rec := happyRecording("a3f9c021000000000000000000000001")
	fp := newFakePlaud(t, []fakeRecording{rec})

	now := time.Date(2026, 5, 1, 9, 14, 21, 0, time.UTC)
	cmd := newSyncCmd(
		withSyncBaseURLResolver(func(_ api.Region) (string, error) { return fp.api.URL, nil }),
		withSyncNow(func() time.Time { return now }),
	)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--format", "json"})
	_ = cmd.Execute()

	// Look for a done event in the NDJSON stream with status interrupted.
	sawDone := false
	sawInterrupted := false
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		if line == "" {
			continue
		}
		var env map[string]any
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			continue
		}
		if env["event"] == "done" {
			sawDone = true
			if details, ok := env["details"].(map[string]any); ok && details["status"] == "interrupted" {
				sawInterrupted = true
			}
		}
	}
	if !sawDone {
		t.Errorf("expected at least one done event in NDJSON stream:\n%s", stdout.String())
	}
	if !sawInterrupted {
		t.Errorf("expected done event with details.status=\"interrupted\":\n%s", stdout.String())
	}
}

// ---------------------------------------------------------------------------
// F-09: --prune only with flag
// ---------------------------------------------------------------------------

func TestSync_F09_PruneOnlyWithFlag(t *testing.T) {
	setTempConfig(t)
	seedCreds(t)
	root := withTempArchive(t)

	rec := happyRecording("a3f9c021000000000000000000000001")
	fp := newFakePlaud(t, []fakeRecording{rec})

	// First sync: fetches the recording.
	if _, _, err := runSyncCmd(t, fp); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	// Now "delete" the recording from the server. We can do this by spinning
	// up a new fake with no recordings; the existing on-disk archive remains.
	fp2 := newFakePlaud(t, nil)

	// Without --prune: folder remains.
	if _, _, err := runSyncCmdAtRoot(t, fp2, root); err != nil {
		t.Fatalf("sync without --prune: %v", err)
	}
	if !hasMetadataUnder(root) {
		t.Errorf("recording folder removed without --prune")
	}

	// With --prune-empty (because we've gone from 1 to 0 recordings,
	// triggering the empty-server guard).
	stdout, stderr, err := runSyncCmdAtRoot(t, fp2, root, "--prune", "--prune-empty")
	if err != nil {
		t.Fatalf("sync --prune --prune-empty: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, ".trash", rec.id)); err != nil {
		t.Errorf(".trash/%s/ missing after prune: %v", rec.id, err)
	}
}

// runSyncCmdAtRoot is a variant of runSyncCmd that pins archive root to a
// known directory (so we can sync, then re-sync against the same root).
func runSyncCmdAtRoot(t *testing.T, fp *fakePlaudServer, root string, extraArgs ...string) (string, string, error) {
	t.Helper()
	t.Setenv("PLAUD_ARCHIVE_DIR", root)
	now := time.Date(2026, 5, 1, 9, 14, 21, 0, time.UTC)
	cmd := newSyncCmd(
		withSyncBaseURLResolver(func(_ api.Region) (string, error) { return fp.api.URL, nil }),
		withSyncNow(func() time.Time { return now }),
	)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())
	cmd.SetArgs(extraArgs)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func hasMetadataUnder(root string) bool {
	found := false
	_ = filepath.Walk(root, func(p string, info os.FileInfo, _ error) error {
		if info != nil && info.Name() == "metadata.json" {
			// Skip ones inside .trash/
			if !strings.Contains(p, string(os.PathSeparator)+".trash"+string(os.PathSeparator)) {
				found = true
			}
		}
		return nil
	})
	return found
}

// ---------------------------------------------------------------------------
// F-12: dry-run touches no recording folder
// ---------------------------------------------------------------------------

func TestSync_F12_DryRunNoMutation(t *testing.T) {
	setTempConfig(t)
	seedCreds(t)
	root := withTempArchive(t)

	rec := happyRecording("a3f9c021000000000000000000000001")
	fp := newFakePlaud(t, []fakeRecording{rec})

	stdout, _, err := runSyncCmd(t, fp, "--dry-run", "--format", "json")
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}

	// State file was created (last_run_started checkpoint).
	if _, err := os.Stat(filepath.Join(root, ".plaud-sync.state")); err != nil {
		t.Errorf(".plaud-sync.state should have been created: %v", err)
	}
	// No metadata.json anywhere.
	if hasMetadataUnder(root) {
		t.Errorf("dry-run created metadata.json")
	}
	// NDJSON output should contain a would-fetch event.
	if !strings.Contains(stdout, `"event":"would-fetch"`) {
		t.Errorf("dry-run NDJSON should contain would-fetch event:\n%s", stdout)
	}
}

// ---------------------------------------------------------------------------
// F-13: redaction at NDJSON and stderr surfaces
// ---------------------------------------------------------------------------

func TestSync_F13_NoSignedURLLeaksToNDJSON(t *testing.T) {
	setTempConfig(t)
	seedCreds(t)
	withTempArchive(t)

	rec := happyRecording("a3f9c021000000000000000000000001")
	fp := newFakePlaud(t, []fakeRecording{rec})
	// Force the audio leg to reject so an error containing the signed URL
	// flows through. Reuse the audioForce403Once flag.
	fp.audioForce403Once.Store(true)

	stdout, _, err := runSyncCmd(t, fp, "--include", "audio,transcript,summary,metadata", "--format", "json")
	_ = err // we expect either ok or per-recording failure; just check for no leaks
	for _, leak := range []string{"X-Amz-Signature", "X-Amz-Credential", "amazonaws.com", "AKIA"} {
		if strings.Contains(stdout, leak) {
			t.Errorf("F-13: NDJSON leaked %q\nstdout=%s", leak, stdout)
		}
	}
}

func TestSync_F13_NoBearerTokenLeaksToStderr(t *testing.T) {
	setTempConfig(t)
	seedCreds(t)
	withTempArchive(t)

	rec := happyRecording("a3f9c021000000000000000000000001")
	fp := newFakePlaud(t, []fakeRecording{rec})

	// Forge a state file with a bearer token in last_error so a save+load
	// roundtrip exercises redaction. Then sync; the redacted error must not
	// appear in stderr.
	root := os.Getenv("PLAUD_ARCHIVE_DIR")
	statePath := filepath.Join(root, ".plaud-sync.state")
	corrupt := `{"schema_version":1,"recordings":{"x":{"version":"v","folder_path":"a","last_error":{"msg":"Bearer eyJabc.def.ghi","at":"2026-05-01T00:00:00Z"}}}}`
	if err := os.WriteFile(statePath, []byte(corrupt), 0o644); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	_, stderr, err := runSyncCmd(t, fp)
	_ = err
	if strings.Contains(stderr, "eyJabc.def.ghi") {
		t.Errorf("F-13: stderr leaked Bearer JWT:\n%s", stderr)
	}
}

// ---------------------------------------------------------------------------
// F-05: watch mode
// ---------------------------------------------------------------------------

// runWatchCmdWithCancel runs the sync command in --watch mode with a sleep
// override that increments a counter and waits for the test to advance.
// The test cancels ctx after the desired number of cycles.
func runWatchCmdWithCancel(t *testing.T, fp *fakePlaudServer, archiveRoot string, opts ...struct {
	failure bool
}) (cycles int, stdout, stderr string, err error) {
	t.Helper()
	t.Setenv("PLAUD_ARCHIVE_DIR", archiveRoot)

	now := time.Date(2026, 5, 1, 9, 14, 21, 0, time.UTC)
	cycleCh := make(chan struct{}, 100)
	cmd := newSyncCmd(
		withSyncBaseURLResolver(func(_ api.Region) (string, error) { return fp.api.URL, nil }),
		withSyncNow(func() time.Time { return now }),
		withSyncSleep(func(_ time.Duration) <-chan time.Time {
			cycleCh <- struct{}{}
			ch := make(chan time.Time, 1)
			ch <- time.Now()
			return ch
		}),
	)
	var so, se bytes.Buffer
	cmd.SetOut(&so)
	cmd.SetErr(&se)
	ctx, cancel := context.WithCancel(context.Background())
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--watch", "--interval", "10ms", "--format", "json"})

	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()

	// Wait for at least 3 cycles, then cancel.
	for i := 0; i < 3; i++ {
		select {
		case <-cycleCh:
			cycles++
		case <-time.After(2 * time.Second):
			cancel()
			t.Fatalf("watch did not produce 3 cycles within 2s; cycles seen=%d\nstdout=%s\nstderr=%s", cycles, so.String(), se.String())
		}
	}
	cancel()
	select {
	case err = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watch did not exit within 2s after cancel")
	}
	// Drain remaining cycles.
	for {
		select {
		case <-cycleCh:
			cycles++
		default:
			return cycles, so.String(), se.String(), err
		}
	}
}

func TestSync_F05_WatchRunsMultipleCycles(t *testing.T) {
	setTempConfig(t)
	seedCreds(t)
	root := withTempArchive(t)

	rec := happyRecording("a3f9c021000000000000000000000001")
	fp := newFakePlaud(t, []fakeRecording{rec})

	cycles, stdout, stderr, err := runWatchCmdWithCancel(t, fp, root)
	if err != nil {
		t.Fatalf("watch returned: %v\nstderr=%s", err, stderr)
	}
	if cycles < 3 {
		t.Errorf("cycles=%d, want >=3", cycles)
	}
	// Each cycle emits a done event.
	doneCount := strings.Count(stdout, `"event":"done"`)
	if doneCount < 3 {
		t.Errorf("done events=%d, want >=3", doneCount)
	}
}

func TestSync_F05_WatchExitsOnSIGINT(t *testing.T) {
	// Cancellation == clean exit (no error, no panic).
	setTempConfig(t)
	seedCreds(t)
	root := withTempArchive(t)

	rec := happyRecording("a3f9c021000000000000000000000001")
	fp := newFakePlaud(t, []fakeRecording{rec})

	_, _, _, err := runWatchCmdWithCancel(t, fp, root)
	if err != nil {
		t.Errorf("expected clean exit on cancel, got %v", err)
	}
}

// SIGTERM is mapped to SIGINT by the os.signal.NotifyContext wiring at the
// cmd entry; functionally the cancellation contract is identical. We
// exercise that contract via context.Cancel here; an end-to-end SIGTERM
// test belongs in §8 acceptance.
func TestSync_F05_WatchExitsOnSIGTERM(t *testing.T) {
	setTempConfig(t)
	seedCreds(t)
	root := withTempArchive(t)
	rec := happyRecording("a3f9c021000000000000000000000001")
	fp := newFakePlaud(t, []fakeRecording{rec})
	if _, _, _, err := runWatchCmdWithCancel(t, fp, root); err != nil {
		t.Errorf("SIGTERM-equivalent cancel: got err %v", err)
	}
}

func TestSync_F05_WatchIntervalIsSleepDurationNotWallClock(t *testing.T) {
	// We measure that o.sleep is called once per cycle with the configured
	// interval. The runWatchCmdWithCancel helper records the duration via
	// the cycle channel; deeper assertion would require a richer harness.
	setTempConfig(t)
	seedCreds(t)
	root := withTempArchive(t)

	rec := happyRecording("a3f9c021000000000000000000000001")
	fp := newFakePlaud(t, []fakeRecording{rec})

	now := time.Date(2026, 5, 1, 9, 14, 21, 0, time.UTC)
	var (
		sleepMu        sync.Mutex
		sleepDurations []time.Duration
	)
	cmd := newSyncCmd(
		withSyncBaseURLResolver(func(_ api.Region) (string, error) { return fp.api.URL, nil }),
		withSyncNow(func() time.Time { return now }),
		withSyncSleep(func(d time.Duration) <-chan time.Time {
			sleepMu.Lock()
			sleepDurations = append(sleepDurations, d)
			sleepMu.Unlock()
			ch := make(chan time.Time, 1)
			ch <- time.Now()
			return ch
		}),
	)
	t.Setenv("PLAUD_ARCHIVE_DIR", root)
	var so, se bytes.Buffer
	cmd.SetOut(&so)
	cmd.SetErr(&se)
	ctx, cancel := context.WithCancel(context.Background())
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--watch", "--interval", "150ms", "--format", "json"})

	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sleepMu.Lock()
		n := len(sleepDurations)
		sleepMu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	sleepMu.Lock()
	defer sleepMu.Unlock()
	if len(sleepDurations) < 2 {
		t.Fatalf("expected >=2 sleep calls, got %d", len(sleepDurations))
	}
	for _, d := range sleepDurations {
		if d != 150*time.Millisecond {
			t.Errorf("sleep duration=%v, want 150ms (sleep-duration interval, F-05)", d)
		}
	}
}

func TestSync_F05_WatchExitsAfter5ConsecutiveFailures(t *testing.T) {
	setTempConfig(t)
	seedCreds(t)
	root := withTempArchive(t)

	// Force every list call to 401 so every cycle errors.
	rec := happyRecording("a3f9c021000000000000000000000001")
	fp := newFakePlaud(t, []fakeRecording{rec})
	fp.listForce401.Store(true)

	now := time.Date(2026, 5, 1, 9, 14, 21, 0, time.UTC)
	cmd := newSyncCmd(
		withSyncBaseURLResolver(func(_ api.Region) (string, error) { return fp.api.URL, nil }),
		withSyncNow(func() time.Time { return now }),
		withSyncSleep(func(_ time.Duration) <-chan time.Time {
			ch := make(chan time.Time, 1)
			ch <- time.Now()
			return ch
		}),
	)
	t.Setenv("PLAUD_ARCHIVE_DIR", root)
	var so, se bytes.Buffer
	cmd.SetOut(&so)
	cmd.SetErr(&se)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--watch", "--interval", "1ms", "--format", "json"})

	exitErr := make(chan error, 1)
	go func() { exitErr <- cmd.Execute() }()

	select {
	case err := <-exitErr:
		if err == nil {
			t.Errorf("expected error after 5 failures, got nil")
		}
		if !strings.Contains(se.String(), "Watch loop has failed") {
			t.Errorf("expected stderr to mention failure threshold:\n%s", se.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("watch did not exit within 3s; stdout=%s stderr=%s", so.String(), se.String())
	}
}

// ---------------------------------------------------------------------------
// F-14: default --include omits audio
// ---------------------------------------------------------------------------

func TestSync_F14_DefaultIncludeOmitsAudio(t *testing.T) {
	setTempConfig(t)
	seedCreds(t)
	root := withTempArchive(t)

	rec := happyRecording("a3f9c021000000000000000000000001")
	fp := newFakePlaud(t, []fakeRecording{rec})

	if _, _, err := runSyncCmd(t, fp); err != nil {
		t.Fatalf("runSync: %v", err)
	}

	// Find the recording folder; assert no audio.mp3.
	var folder string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, _ error) error {
		if info != nil && info.Name() == "metadata.json" {
			folder = filepath.Dir(p)
		}
		return nil
	})
	if folder == "" {
		t.Fatalf("no recording folder created under %s", root)
	}
	if _, err := os.Stat(filepath.Join(folder, "audio.mp3")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("F-14: audio.mp3 should not exist by default; stat err=%v", err)
	}
	// Transcript and summary should exist.
	if _, err := os.Stat(filepath.Join(folder, "transcript.json")); err != nil {
		t.Errorf("transcript.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(folder, "summary.plaud.md")); err != nil {
		t.Errorf("summary.plaud.md missing: %v", err)
	}
}
