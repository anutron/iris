package argus

import (
	"sync"
	"sync/atomic"
)

// LinkState is the current health of iris's connection to the argus REST
// API. The zero value is LinkHealthy so package-level state starts healthy
// without initialization.
type LinkState int32

const (
	LinkHealthy LinkState = iota
	LinkRecovering
	LinkDown
)

// String returns the lowercase wire string used in error messages.
func (s LinkState) String() string {
	switch s {
	case LinkHealthy:
		return "healthy"
	case LinkRecovering:
		return "recovering"
	case LinkDown:
		return "down"
	default:
		return "unknown"
	}
}

var state atomic.Int32

var (
	lastErrMu sync.RWMutex
	lastErr   error
)

// GetLinkState returns the current link state. Safe for concurrent use.
func GetLinkState() LinkState { return LinkState(state.Load()) }

// SetLinkState updates the current link state. Safe for concurrent use.
func SetLinkState(s LinkState) { state.Store(int32(s)) }

// LinkLastError returns the most recent error stored by SetLinkError, or
// nil if none has been recorded since the last clear.
func LinkLastError() error {
	lastErrMu.RLock()
	defer lastErrMu.RUnlock()
	return lastErr
}

// SetLinkError records the error driving a transition to LinkDown. Pass
// nil to clear the stored error when transitioning back to healthy.
func SetLinkError(err error) {
	lastErrMu.Lock()
	lastErr = err
	lastErrMu.Unlock()
}
