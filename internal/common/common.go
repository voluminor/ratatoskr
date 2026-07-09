package common

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
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

// DeadlineAction is the outcome of the shared idle/write deadline half-refresh gate.
type DeadlineAction uint8

const (
	// DeadlineSkip — current deadline still has more than half its budget; no syscall.
	DeadlineSkip DeadlineAction = iota
	// DeadlineArm — re-arm the deadline to the returned time.
	DeadlineArm
	// DeadlineClear — timeout disabled; clear any existing deadline.
	DeadlineClear
)

// DeadlineRefresh implements the half-refresh gate shared by SOCKS tunnels and
// UDP/TCP forwarders: it avoids the SetDeadline syscall while the current
// deadline still has more than half its budget left. timeout must already be
// normalized; timeout <= 0 disables the deadline. currentNanos is the last armed
// deadline (UnixNano, 0 when none). The caller applies the returned action to its
// own conn and deadline field, keeping ownership of atomic/mutex synchronization.
func DeadlineRefresh(now time.Time, timeout, previousTimeout time.Duration, currentNanos int64) (DeadlineAction, time.Time) {
	if timeout <= 0 {
		return DeadlineClear, time.Time{}
	}
	refreshAfter := timeout / 2
	if refreshAfter <= 0 {
		refreshAfter = timeout
	}
	refreshAt := now.Add(refreshAfter).UnixNano()
	if previousTimeout == timeout && currentNanos > refreshAt {
		return DeadlineSkip, time.Time{}
	}
	return DeadlineArm, now.Add(timeout)
}
