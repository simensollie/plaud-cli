package sync

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLock_F11_SecondInvocationFails(t *testing.T) {
	root := t.TempDir()
	first, holder, err := AcquireLock(root)
	if err != nil {
		t.Fatalf("first AcquireLock: %v", err)
	}
	t.Cleanup(func() { _ = first.Release() })
	if holder == nil || holder.PID != os.Getpid() {
		t.Errorf("first holder PID=%d, want %d", holder.PID, os.Getpid())
	}

	second, h2, err := AcquireLock(root)
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("second AcquireLock err=%v, want ErrLocked", err)
	}
	if second != nil {
		t.Errorf("second AcquireLock returned non-nil lock")
	}
	if h2 == nil {
		t.Fatalf("second AcquireLock did not return holder info")
	}
	if h2.PID != os.Getpid() {
		t.Errorf("contention holder PID=%d, want %d", h2.PID, os.Getpid())
	}
}

func TestLock_F11_StructuredContentionMessage(t *testing.T) {
	root := t.TempDir()
	first, _, err := AcquireLock(root)
	if err != nil {
		t.Fatalf("first AcquireLock: %v", err)
	}
	t.Cleanup(func() { _ = first.Release() })

	_, h, err := AcquireLock(root)
	if err == nil {
		t.Fatal("expected ErrLocked")
	}
	if h == nil || h.PID == 0 || h.Hostname == "" || h.StartedAt.IsZero() {
		t.Errorf("contention holder missing structured fields: %+v", h)
	}
}

func TestLock_F11_ReleaseAllowsReacquire(t *testing.T) {
	root := t.TempDir()
	first, _, err := AcquireLock(root)
	if err != nil {
		t.Fatalf("first AcquireLock: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("first.Release: %v", err)
	}

	second, _, err := AcquireLock(root)
	if err != nil {
		t.Fatalf("second AcquireLock after release: %v", err)
	}
	t.Cleanup(func() { _ = second.Release() })
}

func TestLock_F11_LockFileLandsAtKnownPath(t *testing.T) {
	root := t.TempDir()
	lock, _, err := AcquireLock(root)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	if _, err := os.Stat(filepath.Join(root, lockFilename)); err != nil {
		t.Errorf("lock file missing: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Watch sentinel
// ---------------------------------------------------------------------------

func TestLock_F11_TwoWatchesDetectedViaSentinel(t *testing.T) {
	root := t.TempDir()
	first, _, err := AcquireWatchSentinel(root)
	if err != nil {
		t.Fatalf("first AcquireWatchSentinel: %v", err)
	}
	t.Cleanup(func() { _ = first.Release() })

	_, holder, err := AcquireWatchSentinel(root)
	if !errors.Is(err, ErrWatchActive) {
		t.Fatalf("second AcquireWatchSentinel err=%v, want ErrWatchActive", err)
	}
	if holder == nil || holder.PID == 0 {
		t.Errorf("expected holder info, got %+v", holder)
	}
}

func TestLock_F11_StaleSentinelTakenOver(t *testing.T) {
	root := t.TempDir()
	// Seed a sentinel claiming to be a long-dead PID on this host.
	hostname, _ := os.Hostname()
	stale := &LockHolder{PID: 1, Hostname: hostname}
	if err := writeHolderFile(filepath.Join(root, watchSentinelFilename), stale); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// PID 1 is init/systemd and is alive on Linux/Mac, which would prevent
	// the take-over. Use a guaranteed-not-alive PID instead.
	stale.PID = 999999999
	if err := writeHolderFile(filepath.Join(root, watchSentinelFilename), stale); err != nil {
		t.Fatalf("seed: %v", err)
	}

	w, holder, err := AcquireWatchSentinel(root)
	if err != nil {
		t.Fatalf("expected stale-sentinel takeover, got %v", err)
	}
	t.Cleanup(func() { _ = w.Release() })
	if holder == nil || holder.PID != os.Getpid() {
		t.Errorf("new holder=%+v, want PID=%d", holder, os.Getpid())
	}
}

// Sanity: contention message construction is grep-able.
func TestLock_F11_HolderInfoSurfaceableInMessage(t *testing.T) {
	h := &LockHolder{PID: 12345, Hostname: "kingsnake.local"}
	msg := "lock held by PID " + itoa(h.PID) + " on " + h.Hostname
	if !strings.Contains(msg, "12345") || !strings.Contains(msg, "kingsnake.local") {
		t.Errorf("message %q lacks expected substrings", msg)
	}
}

// itoa avoids importing strconv into the test file just for one line.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf []byte
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
