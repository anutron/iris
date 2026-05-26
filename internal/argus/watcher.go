package argus

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultWatcherInterval is the polling cadence used by the daemon.
const DefaultWatcherInterval = 1 * time.Second

// Watcher polls argus's pid file mtime and socket ping on a fixed interval
// and invokes OnRestart whenever either signal indicates a restart.
// Concurrent triggers are coalesced via single-flight: while a callback is
// in flight, further polling ticks do not invoke OnRestart again.
type Watcher struct {
	PidPath   string
	Ping      func(ctx context.Context) error
	Interval  time.Duration
	OnRestart func(context.Context)
	Log       *slog.Logger

	stop     chan struct{}
	wg       sync.WaitGroup
	inflight atomic.Bool

	once sync.Once
}

// Start launches the polling goroutine. Idempotent past the first call.
// ctx bounds the lifetime of the polling loop AND every restart callback
// it spawns.
func (w *Watcher) Start(ctx context.Context) {
	w.once.Do(func() {
		w.stop = make(chan struct{})
		interval := w.Interval
		if interval <= 0 {
			interval = DefaultWatcherInterval
		}

		w.wg.Add(1)
		go w.loop(ctx, interval)
	})
}

// Stop signals the polling loop to exit and waits for it (and any
// in-flight callback) to finish, bounded by ctx.
func (w *Watcher) Stop(ctx context.Context) {
	if w.stop == nil {
		return
	}
	select {
	case <-w.stop:
	default:
		close(w.stop)
	}

	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func (w *Watcher) loop(ctx context.Context, interval time.Duration) {
	defer w.wg.Done()

	var lastMtime time.Time
	if fi, err := os.Stat(w.PidPath); err == nil {
		lastMtime = fi.ModTime()
	} else if w.Log != nil {
		w.Log.Debug("watcher: initial pid stat failed", "path", w.PidPath, "err", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stop:
			return
		case <-ticker.C:
			lastMtime = w.tick(ctx, lastMtime)
		}
	}
}

func (w *Watcher) tick(ctx context.Context, lastMtime time.Time) time.Time {
	trigger := false

	if fi, err := os.Stat(w.PidPath); err == nil {
		if !fi.ModTime().Equal(lastMtime) {
			trigger = true
			lastMtime = fi.ModTime()
		}
	} else if w.Log != nil {
		w.Log.Debug("watcher: pid stat failed", "path", w.PidPath, "err", err)
	}

	if err := w.Ping(ctx); err != nil {
		trigger = true
		if w.Log != nil {
			w.Log.Debug("watcher: ping failed", "err", err)
		}
	}

	if !trigger {
		return lastMtime
	}
	if !w.inflight.CompareAndSwap(false, true) {
		return lastMtime
	}

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		defer w.inflight.Store(false)
		w.OnRestart(ctx)
	}()
	return lastMtime
}
