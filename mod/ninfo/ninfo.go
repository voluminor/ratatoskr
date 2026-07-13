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
	source         SourceInterface
	nodeInfo       yggcore.AddHandlerFunc
	ctx            context.Context
	cancel         context.CancelFunc
	closeOnce      sync.Once
	maxAskTime     time.Duration
	askRetryPause  time.Duration
	lookupInterval time.Duration
	maxLookupTime  time.Duration
	sigilsMu       sync.RWMutex
	sigils         map[string]sigils.Interface
	askMu          sync.Mutex
	askFlights     map[[ed25519.PublicKeySize]byte]*askFlightObj
	askWG          sync.WaitGroup
	closed         bool
}

type askFlightObj struct {
	done chan struct{}
	raw  json.RawMessage
	rtt  time.Duration
	err  error
}

type ConfigObj struct {
	// Source is the running core.
	Source SourceInterface

	// Timing for remote NodeInfo queries; 0 → the internal default for each field.
	// MaxAskTime bounds a full Ask when the caller sets no deadline; AskRetryPause
	// is the wait between Ask attempts (<0 disables retries); LookupInterval is the
	// initial address-lookup poll interval; MaxLookupTime bounds an address lookup
	// when the caller sets no deadline.
	MaxAskTime     time.Duration
	AskRetryPause  time.Duration
	LookupInterval time.Duration
	MaxLookupTime  time.Duration
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
	defaultMaxAskTime     = 30 * time.Second
	defaultAskRetryPause  = 500 * time.Millisecond
	defaultLookupInterval = 100 * time.Millisecond
	defaultMaxLookupTime  = 30 * time.Second
	maxConcurrentAsks     = 64
)

// //

func orDefaultDuration(v, def time.Duration) time.Duration {
	if v == 0 {
		return def
	}
	return v
}

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
		source:         cfg.Source,
		nodeInfo:       nodeInfo,
		ctx:            ctx,
		cancel:         cancel,
		maxAskTime:     orDefaultDuration(cfg.MaxAskTime, defaultMaxAskTime),
		askRetryPause:  orDefaultDuration(cfg.AskRetryPause, defaultAskRetryPause),
		lookupInterval: orDefaultDuration(cfg.LookupInterval, defaultLookupInterval),
		maxLookupTime:  orDefaultDuration(cfg.MaxLookupTime, defaultMaxLookupTime),
		sigils:         make(map[string]sigils.Interface),
		askFlights:     make(map[[ed25519.PublicKeySize]byte]*askFlightObj),
	}
	return obj, nil
}

// Close releases resources held by the module.
// Close cancels shared work and waits for accepted Ask flights to leave the
// captured handler. Standalone Close intentionally waits for accepted work; the
// root ratatoskr object bounds its aggregate shutdown with ConfigObj.CloseTimeout.
func (obj *Obj) Close() error {
	obj.closeOnce.Do(func() {
		obj.askMu.Lock()
		obj.closed = true
		if obj.cancel != nil {
			obj.cancel()
		}
		obj.askMu.Unlock()
		obj.askWG.Wait()
	})
	return nil
}

// // // // // // // // // //

func ensureCallerContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (obj *Obj) isClosed() bool {
	obj.askMu.Lock()
	closed := obj.closedLocked()
	obj.askMu.Unlock()
	return closed
}

func (obj *Obj) closedLocked() bool {
	// A zero-value Obj (never went through New) has no context; treat as closed.
	return obj.closed || obj.ctx == nil
}

// Ask queries a remote node's NodeInfo by its public key.
// The captured getNodeInfo handler is called synchronously in the caller's
// goroutine and retried after askRetryPause until the caller context expires or
// the module is closed. The underlying Yggdrasil handler has its own hard 6 s
// timeout per attempt, which often fires before routing converges on a freshly
// started node, so retrying is what lets the address eventually resolve.
func (obj *Obj) Ask(ctx context.Context, key ed25519.PublicKey) (*AskResultObj, error) {
	if len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: got %d, expected %d", ErrInvalidKeyLength, len(key), ed25519.PublicKeySize)
	}
	if obj.isClosed() {
		return nil, ErrClosed
	}
	ctx = ensureCallerContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var ka [ed25519.PublicKeySize]byte
	copy(ka[:], key)

	obj.askMu.Lock()
	if obj.closedLocked() {
		obj.askMu.Unlock()
		return nil, ErrClosed
	}
	if obj.askFlights == nil {
		obj.askFlights = make(map[[ed25519.PublicKeySize]byte]*askFlightObj)
	}
	if flight := obj.askFlights[ka]; flight != nil {
		obj.askMu.Unlock()
		return obj.waitAskFlight(ctx, flight)
	}
	if len(obj.askFlights) >= maxConcurrentAsks {
		obj.askMu.Unlock()
		return nil, ErrAskBusy
	}
	flight := &askFlightObj{done: make(chan struct{})}
	obj.askFlights[ka] = flight
	obj.askWG.Add(1)
	obj.askMu.Unlock()
	go obj.runAskFlight(ka, flight)
	return obj.waitAskFlight(ctx, flight)
}

func (obj *Obj) waitAskFlight(ctx context.Context, flight *askFlightObj) (*AskResultObj, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-flight.done:
		if flight.err != nil {
			return nil, flight.err
		}
		return obj.parseAskResponse(flight.raw, flight.rtt)
	}
}

func (obj *Obj) runAskFlight(key [ed25519.PublicKeySize]byte, flight *askFlightObj) {
	defer obj.askWG.Done()
	defer func() {
		obj.askMu.Lock()
		if obj.askFlights[key] == flight {
			delete(obj.askFlights, key)
		}
		obj.askMu.Unlock()
		close(flight.done)
	}()

	workCtx := obj.ctx
	cancel := func() {}
	if obj.maxAskTime > 0 {
		workCtx, cancel = context.WithTimeout(workCtx, obj.maxAskTime)
	}
	defer cancel()
	retryPause := obj.askRetryPause
	var lastErr error
	for {
		start := time.Now()
		raw, err := obj.callNodeInfo(key)
		if err == nil {
			flight.raw = append(json.RawMessage(nil), raw...)
			flight.rtt = time.Since(start)
			return
		}
		lastErr = err
		if retryPause < 0 {
			flight.err = lastErr
			return
		}
		timer := time.NewTimer(retryPause)
		select {
		case <-workCtx.Done():
			timer.Stop()
			if errors.Is(workCtx.Err(), context.Canceled) && obj.isClosed() {
				flight.err = ErrClosed
			} else {
				flight.err = lastErr
			}
			return
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

// AddSigil registers third-party sigils used by Ask/AskAddr to decode remote
// NodeInfo. Accepts one or many sigils; each self-names via GetName(). Nil,
// invalid-name, reserved, duplicate, or non-cloneable sigils are rejected; the
// collected failures are returned joined (nil if all succeed).
func (obj *Obj) AddSigil(sigs ...sigils.Interface) error {
	var errs []error
	obj.sigilsMu.Lock()
	defer obj.sigilsMu.Unlock()
	for _, si := range sigs {
		if si == nil {
			errs = append(errs, fmt.Errorf("sigil is nil"))
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
		// Store a clone so a caller mutating its own sigil after registration cannot
		// alter parse state; a sigil that cannot clone itself is rejected.
		clone := si.Clone()
		if clone == nil {
			errs = append(errs, fmt.Errorf("sigil[%s] Clone returned nil", name))
			continue
		}
		obj.sigils[name] = clone
	}
	return errors.Join(errs...)
}

// GetSigil returns a clone of the registered sigil, or nil if none is registered.
func (obj *Obj) GetSigil(name string) sigils.Interface {
	obj.sigilsMu.RLock()
	defer obj.sigilsMu.RUnlock()
	if sg := obj.sigils[name]; sg != nil {
		return sg.Clone()
	}
	return nil
}

// DelSigil removes a registered sigil by name.
func (obj *Obj) DelSigil(name string) error {
	obj.sigilsMu.Lock()
	defer obj.sigilsMu.Unlock()
	if _, ok := obj.sigils[name]; !ok {
		return fmt.Errorf("sigil[%s] not found", name)
	}
	delete(obj.sigils, name)
	return nil
}

// ImportSigils registers all non-reserved sigils assembled in a sigil_core.Obj.
func (obj *Obj) ImportSigils(src *sigil_core.Obj) error {
	obj.sigilsMu.Lock()
	defer obj.sigilsMu.Unlock()
	var errs []error
	if src == nil {
		return fmt.Errorf("sigil source is nil")
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
	return errors.Join(errs...)
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
