package argus

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// Watcher fires OnRestart when the pid mtime changes.
func TestWatcher_FiresOnPidMtimeChange(t *testing.T) {
	tmp := t.TempDir()
	pidPath := filepath.Join(tmp, "daemon.pid")
	if err := os.WriteFile(pidPath, []byte("123"), 0o644); err != nil {
		t.Fatalf("write pid: %v", err)
	}

	var fired atomic.Int32
	w := &Watcher{
		PidPath:   pidPath,
		Ping:      func(context.Context) error { return nil },
		Interval:  10 * time.Millisecond,
		OnRestart: func(context.Context) { fired.Add(1) },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	defer w.Stop(context.Background())

	time.Sleep(50 * time.Millisecond) // settle baseline mtime
	// Rewrite pid with a different mtime — explicitly chtimes to ensure
	// a stat-visible change even on fast filesystems.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(pidPath, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for fired.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if fired.Load() == 0 {
		t.Fatal("OnRestart never fired after pid mtime change")
	}
}

// Watcher fires OnRestart when Ping returns an error.
func TestWatcher_FiresOnPingError(t *testing.T) {
	tmp := t.TempDir()
	pidPath := filepath.Join(tmp, "daemon.pid")
	_ = os.WriteFile(pidPath, []byte("123"), 0o644)

	var pingShouldFail atomic.Bool
	var fired atomic.Int32
	w := &Watcher{
		PidPath: pidPath,
		Ping: func(context.Context) error {
			if pingShouldFail.Load() {
				return errors.New("argus down")
			}
			return nil
		},
		Interval:  10 * time.Millisecond,
		OnRestart: func(context.Context) { fired.Add(1) },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	defer w.Stop(context.Background())

	time.Sleep(50 * time.Millisecond)
	pingShouldFail.Store(true)

	deadline := time.Now().Add(2 * time.Second)
	for fired.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if fired.Load() == 0 {
		t.Fatal("OnRestart never fired after ping error")
	}
}

// Watcher coalesces concurrent triggers (single-flight): while an
// OnRestart callback is in flight, further ticks do NOT re-fire.
func TestWatcher_CoalescesConcurrentTriggers(t *testing.T) {
	tmp := t.TempDir()
	pidPath := filepath.Join(tmp, "daemon.pid")
	_ = os.WriteFile(pidPath, []byte("123"), 0o644)

	var firing atomic.Bool
	var fired atomic.Int32
	w := &Watcher{
		PidPath:  pidPath,
		Ping:     func(context.Context) error { return errors.New("always fail") }, // every tick triggers
		Interval: 5 * time.Millisecond,
		OnRestart: func(context.Context) {
			firing.Store(true)
			time.Sleep(200 * time.Millisecond)
			fired.Add(1)
			firing.Store(false)
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	defer w.Stop(context.Background())

	// Let several ticks fire — at most one OnRestart should be running.
	time.Sleep(100 * time.Millisecond)
	if !firing.Load() {
		t.Fatal("expected OnRestart to be in flight during the test window")
	}

	time.Sleep(300 * time.Millisecond)
	// After ~400ms with the callback taking 200ms each, we should have
	// fired ~2 times — not 80. Anything ≥ 5 would mean single-flight broke.
	if got := fired.Load(); got > 4 {
		t.Fatalf("expected single-flight coalescing; OnRestart fired %d times", got)
	}
}
