package common

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

const (
	PublicKeyDomainSuffix = ".pk.ygg"
	publicKeyHexLength    = ed25519.PublicKeySize * 2
)

var (
	ErrInvalidPublicKeyDomain = errors.New("invalid public key domain")
	ErrInvalidPublicKeyLength = errors.New("invalid public key length")
)

// //

func ParsePublicKeyDomain(name string) (ed25519.PublicKey, bool, error) {
	if len(name) < len(PublicKeyDomainSuffix) || !strings.EqualFold(name[len(name)-len(PublicKeyDomainSuffix):], PublicKeyDomainSuffix) {
		return nil, false, nil
	}
	hexKey := name[:len(name)-len(PublicKeyDomainSuffix)]
	if len(hexKey) != publicKeyHexLength {
		return nil, true, fmt.Errorf("%w: expected %d hex characters, got %d", ErrInvalidPublicKeyLength, publicKeyHexLength, len(hexKey))
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, true, fmt.Errorf("%w: %w", ErrInvalidPublicKeyDomain, err)
	}
	return ed25519.PublicKey(key), true, nil
}

// //

// DiscardLoggerObj preserves nil-logger contracts without branching at call sites.
type DiscardLoggerObj struct{}

func (DiscardLoggerObj) Printf(string, ...interface{}) {}
func (DiscardLoggerObj) Println(...interface{})        {}
func (DiscardLoggerObj) Infof(string, ...interface{})  {}
func (DiscardLoggerObj) Infoln(...interface{})         {}
func (DiscardLoggerObj) Warnf(string, ...interface{})  {}
func (DiscardLoggerObj) Warnln(...interface{})         {}
func (DiscardLoggerObj) Errorf(string, ...interface{}) {}
func (DiscardLoggerObj) Errorln(...interface{})        {}
func (DiscardLoggerObj) Debugf(string, ...interface{}) {}
func (DiscardLoggerObj) Debugln(...interface{})        {}
func (DiscardLoggerObj) Traceln(...interface{})        {}

func NormalizeLogger(log yggcore.Logger) yggcore.Logger {
	if log == nil {
		return DiscardLoggerObj{}
	}
	return log
}

// //

// AdminCaptureObj intercepts handlers registered through yggdrasil SetAdmin.
type AdminCaptureObj struct {
	Handlers map[string]yggcore.AddHandlerFunc
}

func NewAdminCapture() *AdminCaptureObj {
	return &AdminCaptureObj{Handlers: make(map[string]yggcore.AddHandlerFunc)}
}

func (a *AdminCaptureObj) AddHandler(name, _ string, _ []string, fn yggcore.AddHandlerFunc) error {
	a.Handlers[name] = fn
	return nil
}

// //

// DynamicLimitObj is a semaphore whose limit can change at runtime; limit <= 0 means unlimited.
type DynamicLimitObj struct {
	mu     sync.Mutex
	limit  int
	active uint32
	ready  chan struct{}
}

func NewDynamicLimit(limit int) *DynamicLimitObj {
	l := &DynamicLimitObj{}
	l.Set(limit)
	return l
}

func (l *DynamicLimitObj) Set(limit int) {
	l.mu.Lock()
	l.limit = limit
	l.signalLocked()
	l.mu.Unlock()
}

func (l *DynamicLimitObj) Limit() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.limit
}

func (l *DynamicLimitObj) Active() uint32 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.active
}

func (l *DynamicLimitObj) Acquire() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.acquireLocked()
}

func (l *DynamicLimitObj) AcquireOrReady() (bool, <-chan struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.acquireLocked() {
		return true, nil
	}
	return false, l.readyLocked()
}

func (l *DynamicLimitObj) Release() {
	l.mu.Lock()
	if l.active > 0 {
		l.active--
		l.signalLocked()
	}
	l.mu.Unlock()
}

func (l *DynamicLimitObj) acquireLocked() bool {
	if l.limit > 0 && int(l.active) >= l.limit {
		return false
	}
	l.active++
	return true
}

func (l *DynamicLimitObj) readyLocked() chan struct{} {
	if l.ready == nil {
		l.ready = make(chan struct{})
	}
	return l.ready
}

func (l *DynamicLimitObj) signalLocked() {
	if l.ready == nil {
		return
	}
	close(l.ready)
	l.ready = nil
}

// //

// deadlineAction is the outcome of the idle/write deadline half-refresh gate.
type deadlineAction uint8

const (
	// deadlineSkip — current deadline still has more than half its budget; no syscall.
	deadlineSkip deadlineAction = iota
	// deadlineArm — re-arm the deadline to the returned time.
	deadlineArm
	// deadlineClear — timeout disabled; clear any existing deadline.
	deadlineClear
)

// deadlineRefresh is the half-refresh gate: it avoids the SetDeadline syscall
// while the current deadline still has more than half its budget left. timeout
// must already be normalized; timeout <= 0 disables the deadline. currentNanos is
// the last armed deadline (UnixNano, 0 when none).
func deadlineRefresh(now time.Time, timeout time.Duration, currentNanos int64) (deadlineAction, time.Time) {
	if timeout <= 0 {
		return deadlineClear, time.Time{}
	}
	refreshAfter := timeout / 2
	if refreshAfter <= 0 {
		refreshAfter = timeout
	}
	refreshAt := now.Add(refreshAfter).UnixNano()
	if currentNanos > refreshAt {
		return deadlineSkip, time.Time{}
	}
	return deadlineArm, now.Add(timeout)
}

// //

// DeadlineConnInterface is the deadline surface RefreshDeadline drives.
type DeadlineConnInterface interface {
	SetDeadline(time.Time) error
	SetReadDeadline(time.Time) error
}

// RefreshDeadline applies the half-refresh gate against the caller-owned atomic
// deadline state, arming or clearing the connection's idle deadline only when the
// gate says so. readOnly selects SetReadDeadline (one-way UDP relay) over
// SetDeadline (bidirectional tunnel). It is lock-free: concurrent callers may cost
// one redundant SetDeadline syscall but never corrupt state, which is acceptable
// for an advisory idle deadline. timeout must already be normalized.
func RefreshDeadline(now time.Time, timeout time.Duration, state *atomic.Int64, conn DeadlineConnInterface, readOnly bool) {
	action, deadline := deadlineRefresh(now, timeout, state.Load())
	switch action {
	case deadlineArm:
		state.Store(deadline.UnixNano())
		if readOnly {
			_ = conn.SetReadDeadline(deadline)
		} else {
			_ = conn.SetDeadline(deadline)
		}
	case deadlineClear:
		if state.Swap(0) != 0 {
			if readOnly {
				_ = conn.SetReadDeadline(time.Time{})
			} else {
				_ = conn.SetDeadline(time.Time{})
			}
		}
	}
}
