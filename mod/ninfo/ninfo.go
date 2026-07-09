package ninfo

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/voluminor/ratatoskr/internal/common"
	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/mod/sigils/sigil_core"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// Obj holds a reference to the running core.
type Obj struct {
	source            SourceInterface
	nodeInfo          yggcore.AddHandlerFunc
	ctxMu             sync.RWMutex
	ctx               context.Context
	cancel            context.CancelFunc
	closeOnce         sync.Once
	closedFlag        bool
	maxAskTime        time.Duration
	askRetryPause     time.Duration
	lookupInterval    time.Duration
	maxLookupTime     time.Duration
	closeWaitTime     time.Duration
	maxConcurrentAsks int
	askWG             sync.WaitGroup
	askMu             sync.Mutex
	askFlights        map[[ed25519.PublicKeySize]byte]*askFlightObj
	closeErr          error
	lookupMu          sync.Mutex
	lookupFlights     map[string]*addrLookupFlightObj
	unresolvedAddrs   map[string]time.Time
	sigilsMu          sync.RWMutex
	sigils            map[string]sigils.Interface
}

type addrLookupFlightObj struct {
	done chan struct{}
	key  ed25519.PublicKey
	err  error
}

type ConfigObj struct {
	// Source is the running core. NodeInfo timing and limits are fixed internal
	// defaults (see the const block below), so no tuning knobs are exposed.
	Source SourceInterface
}

// SourceInterface is the core access needed by remote NodeInfo lookups.
type SourceInterface interface {
	SetAdmin(yggcore.AddHandler) error
	SendLookup(key ed25519.PublicKey)
	GetPeers() []yggcore.PeerInfo
	GetSessions() []yggcore.SessionInfo
	GetPaths() []yggcore.PathEntryInfo
}

const (
	defaultMaxAskTime        = 30 * time.Second
	defaultAskRetryPause     = 500 * time.Millisecond
	defaultLookupInterval    = 100 * time.Millisecond
	defaultMaxLookupTime     = 30 * time.Second
	defaultCloseWaitTime     = 100 * time.Millisecond
	defaultMaxConcurrentAsks = 256
)

// // // // // // // // // //

// New creates an ninfo module.
// Captures getNodeInfo via core.SetAdmin.
func New(cfg ConfigObj) (*Obj, error) {
	if cfg.Source == nil {
		return nil, ErrCoreRequired
	}

	capture := common.NewAdminCapture()
	if err := cfg.Source.SetAdmin(capture); err != nil {
		return nil, fmt.Errorf("ninfo: capture admin handlers: %w", err)
	}

	nodeInfo := capture.Handlers["getNodeInfo"]
	if nodeInfo == nil {
		return nil, ErrNodeInfoNotCaptured
	}
	ctx, cancel := context.WithCancel(context.Background())

	obj := &Obj{
		source:            cfg.Source,
		nodeInfo:          nodeInfo,
		ctx:               ctx,
		cancel:            cancel,
		maxAskTime:        defaultMaxAskTime,
		askRetryPause:     defaultAskRetryPause,
		lookupInterval:    defaultLookupInterval,
		maxLookupTime:     defaultMaxLookupTime,
		closeWaitTime:     defaultCloseWaitTime,
		maxConcurrentAsks: defaultMaxConcurrentAsks,
		askFlights:        make(map[[ed25519.PublicKeySize]byte]*askFlightObj),
		lookupFlights:     make(map[string]*addrLookupFlightObj),
		sigils:            make(map[string]sigils.Interface),
	}
	return obj, nil
}

// Close releases resources held by the module.
func (obj *Obj) Close() error {
	obj.closeOnce.Do(func() {
		cancel := obj.closeContext()
		if cancel != nil {
			cancel()
		}
		obj.closeErr = obj.waitAskDone(obj.closeWaitTime)
	})
	return obj.closeErr
}

// // // // // // // // // //

func ensureCallerContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (obj *Obj) closeContext() context.CancelFunc {
	obj.ctxMu.Lock()
	defer obj.ctxMu.Unlock()
	obj.closedFlag = true
	return obj.cancel
}

func (obj *Obj) isClosed(ctx context.Context) bool {
	obj.ctxMu.RLock()
	closed := obj.closedFlag
	obj.ctxMu.RUnlock()
	if closed {
		return true
	}
	if ctx == nil {
		return true
	}
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func (obj *Obj) startAskCall() error {
	obj.ctxMu.RLock()
	defer obj.ctxMu.RUnlock()
	if obj.closedFlag || obj.ctx == nil {
		return ErrClosed
	}
	select {
	case <-obj.ctx.Done():
		return ErrClosed
	default:
		obj.askWG.Add(1)
		return nil
	}
}

func (obj *Obj) finishAskCall() {
	obj.askWG.Done()
}

func (obj *Obj) waitAskDone(timeout time.Duration) error {
	if timeout < 0 {
		obj.askWG.Wait()
		return nil
	}
	done := make(chan struct{})
	go func() {
		obj.askWG.Wait()
		close(done)
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return fmt.Errorf("%w after %s", ErrCloseTimedOut, timeout)
	}
}

// askFlightObj is one shared getNodeInfo attempt; its result is published once
// done is closed. Fields are written by the flight goroutine before the close
// and read by waiters after it, so the channel provides the happens-before.
type askFlightObj struct {
	done chan struct{}
	raw  json.RawMessage
	rtt  time.Duration
	err  error
}

// askShared collapses concurrent attempts for the same key into a single
// underlying getNodeInfo call. Only the first caller (leader) launches the call;
// every caller — leader and followers — waits on the shared result under its own
// context, so one caller's cancellation never aborts work the others still need.
// At most maxConcurrentAsks distinct keys can be in flight at once, which stops
// a flood of abandoned unique-key asks from piling up detached goroutines.
// retriable reports whether a failed attempt is worth retrying.
func (obj *Obj) askShared(baseCtx, callCtx context.Context, key [ed25519.PublicKeySize]byte) (json.RawMessage, time.Duration, bool, error) {
	obj.askMu.Lock()
	fl, joined := obj.askFlights[key]
	if !joined {
		// Admission cap: bound the number of distinct in-flight keys so a flood
		// of unique keys with short caller contexts cannot pile up detached
		// goroutines and map entries. Retriable, so a caller with time left
		// waits out a transient burst.
		if obj.maxConcurrentAsks > 0 && len(obj.askFlights) >= obj.maxConcurrentAsks {
			obj.askMu.Unlock()
			return nil, 0, true, ErrAskLimit
		}
		fl = &askFlightObj{done: make(chan struct{})}
		obj.askFlights[key] = fl
	}
	obj.askMu.Unlock()

	if !joined {
		obj.runAskFlight(fl, key)
	}

	select {
	case <-baseCtx.Done():
		return nil, 0, false, ErrClosed
	case <-callCtx.Done():
		return nil, 0, false, callCtx.Err()
	case <-fl.done:
		if fl.err != nil {
			return nil, 0, !errors.Is(fl.err, ErrClosed), fl.err
		}
		return fl.raw, fl.rtt, false, nil
	}
}

// runAskFlight executes one shared getNodeInfo call bounded by the node
// lifetime, so it survives every caller leaving. It holds one askWG count for
// the handler's lifetime, publishes the result, and clears the flight before
// closing done so any retry re-collapses into a fresh flight rather than reusing
// this completed one.
func (obj *Obj) runAskFlight(fl *askFlightObj, key [ed25519.PublicKeySize]byte) {
	go func() {
		defer func() {
			obj.askMu.Lock()
			delete(obj.askFlights, key)
			obj.askMu.Unlock()
			close(fl.done)
		}()
		if err := obj.startAskCall(); err != nil {
			fl.err = err
			return
		}
		defer obj.finishAskCall()
		start := time.Now()
		raw, err := obj.callNodeInfo(key)
		fl.raw, fl.rtt, fl.err = raw, time.Since(start), err
	}()
}

// Ask queries a remote node's NodeInfo by its public key.
// Retries automatically until the context expires, because the underlying
// Yggdrasil handler has a hard 6 s timeout that often fires before
// routing converges on a freshly started node.
// Concurrent asks for the same key share one in-flight call (see askShared),
// so repeated or abandoned asks for a node do not each consume an ask slot.
func (obj *Obj) Ask(ctx context.Context, key ed25519.PublicKey) (*AskResultObj, error) {
	if len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: got %d, expected %d", ErrInvalidKeyLength, len(key), ed25519.PublicKeySize)
	}
	if obj.isClosed(obj.ctx) {
		return nil, ErrClosed
	}
	ctx = ensureCallerContext(ctx)
	if _, ok := ctx.Deadline(); !ok {
		if maxAskTime := obj.maxAskTime; maxAskTime > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, maxAskTime)
			defer cancel()
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var ka [ed25519.PublicKeySize]byte
	copy(ka[:], key)

	// retryPause is fixed for the call: <0 disables retries; the setter never
	// stores 0, so no zero-default branch is needed.
	retryPause := obj.askRetryPause
	var timer *time.Timer
	var lastErr error
	for {
		raw, rtt, retriable, err := obj.askShared(obj.ctx, ctx, ka)
		if err == nil {
			return obj.parseAskResponse(raw, rtt)
		}
		if !retriable {
			if errors.Is(err, ErrClosed) {
				return nil, ErrClosed
			}
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, err
		}
		lastErr = err
		if retryPause < 0 {
			return nil, lastErr
		}
		if timer == nil {
			timer = time.NewTimer(retryPause)
			defer timer.Stop()
		} else {
			timer.Reset(retryPause)
		}
		select {
		case <-obj.ctx.Done():
			return nil, ErrClosed
		case <-ctx.Done():
			return nil, lastErr
		case <-timer.C:
		}
	}
}

// AskAddr resolves an address string to a public key, then calls Ask.
// Supported formats:
//   - "<hex>.pk.ygg" — hex-encoded public key domain
//   - "[ip6]:port" or "ip6" — yggdrasil IPv6 resolved via lookup
//   - raw 64-char hex string — public key directly
func (obj *Obj) AskAddr(ctx context.Context, addr string) (*AskResultObj, error) {
	key, err := obj.resolveAddr(ctx, addr)
	if err != nil {
		return nil, err
	}
	return obj.Ask(ctx, key)
}

// // // // // // // // // //

// AddSigil registers parse sigils used by Ask/AskAddr.
// Invalid or duplicate names are skipped and collected as errors.
func (obj *Obj) AddSigil(sg ...sigils.Interface) []error {
	var errs []error
	obj.sigilsMu.Lock()
	defer obj.sigilsMu.Unlock()
	for i, si := range sg {
		if si == nil {
			errs = append(errs, fmt.Errorf("sigil[%d] is nil", i))
			continue
		}
		name := si.GetName()
		if !sigils.ValidateName(name) {
			errs = append(errs, fmt.Errorf("sigil[%s] is invalid", name))
			continue
		}
		if reservedSigilName(name) {
			errs = append(errs, fmt.Errorf("sigil[%s] is reserved", name))
			continue
		}
		if _, ok := obj.sigils[name]; ok {
			errs = append(errs, fmt.Errorf("duplicated sigil[%s]", name))
			continue
		}
		// Store a clone so a caller mutating its own sigil after registration
		// cannot alter parse state; a sigil that cannot clone itself is rejected
		// rather than shared (matches sigil_core.Add).
		clone := si.Clone()
		if clone == nil {
			errs = append(errs, fmt.Errorf("sigil[%s] Clone returned nil", name))
			continue
		}
		obj.sigils[name] = clone
	}
	return errs
}

// GetSigil returns a registered parse sigil by name, or nil if not found.
func (obj *Obj) GetSigil(name string) sigils.Interface {
	obj.sigilsMu.RLock()
	defer obj.sigilsMu.RUnlock()
	if sg := obj.sigils[name]; sg != nil {
		return sg.Clone()
	}
	return nil
}

// DelSigil removes a parse sigil by name.
func (obj *Obj) DelSigil(name string) error {
	obj.sigilsMu.Lock()
	defer obj.sigilsMu.Unlock()
	if _, ok := obj.sigils[name]; !ok {
		return fmt.Errorf("sigil[%s] not found", name)
	}
	delete(obj.sigils, name)
	return nil
}

// //

// ImportSigils appends sigils from a *sigil_core.Obj into parse sigils.
// Existing names are preserved and returned as conflicts.
func (obj *Obj) ImportSigils(src *sigil_core.Obj) []error {
	obj.sigilsMu.Lock()
	defer obj.sigilsMu.Unlock()
	var errs []error
	if src == nil {
		return []error{fmt.Errorf("sigil source is nil")}
	}
	for name, si := range src.Sigils() {
		if si == nil {
			errs = append(errs, fmt.Errorf("sigil[%s] is nil", name))
			continue
		}
		if reservedSigilName(name) {
			continue
		}
		if _, exists := obj.sigils[name]; exists {
			errs = append(errs, fmt.Errorf("sigil[%s] already exists", name))
			continue
		}
		obj.sigils[name] = si
	}
	return errs
}

// //

func (obj *Obj) sigilSlice() []sigils.Interface {
	obj.sigilsMu.RLock()
	defer obj.sigilsMu.RUnlock()
	if len(obj.sigils) == 0 {
		return nil
	}
	out := make([]sigils.Interface, 0, len(obj.sigils))
	for _, si := range obj.sigils {
		out = append(out, si)
	}
	return out
}
