package ninfo

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"github.com/voluminor/ratatoskr/internal/common"
	"github.com/voluminor/ratatoskr/mod/sigils"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// Obj holds a reference to the running core.
type Obj struct {
	source         SourceInterface
	nodeInfo       yggcore.AddHandlerFunc
	tasks          *common.TaskGroupObj
	maxAskTime     time.Duration
	askRetryPause  time.Duration
	lookupInterval time.Duration
	maxLookupTime  time.Duration
	sigils         []sigils.Interface
	askMu          sync.Mutex
	askFlights     map[[ed25519.PublicKeySize]byte]*askFlightObj
	resolveFlights map[netip.Addr]*resolveFlightObj
	closed         bool
}

type askFlightObj struct {
	done chan struct{}
	raw  json.RawMessage
	rtt  time.Duration
	err  error
}

type resolveFlightObj struct {
	done chan struct{}
	key  ed25519.PublicKey
	err  error
}

type ConfigObj struct {
	// Source is the running core.
	Source SourceInterface

	// Timing for shared remote NodeInfo flights; 0 → the internal default.
	// MaxAskTime stops new retries after its deadline; one synchronous upstream
	// handler already in progress may finish later. MaxLookupTime bounds lookup
	// flights independently of caller cancellation; <0 disables the corresponding
	// deadline. AskRetryPause is the wait between attempts (<0 disables retries).
	// LookupInterval is the initial address-lookup poll interval (<0 is invalid).
	MaxAskTime     time.Duration
	AskRetryPause  time.Duration
	LookupInterval time.Duration
	MaxLookupTime  time.Duration

	// Sigils are trusted custom parsers used for remote NodeInfo. New validates
	// their names and stores one clone; the registry is immutable afterwards.
	Sigils []sigils.Interface
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
	maxConcurrentResolves = 64
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
// Captures getNodeInfo through the configured source.
func New(cfg ConfigObj) (*Obj, error) {
	if cfg.LookupInterval < 0 {
		return nil, fmt.Errorf("%w: got %s", ErrInvalidLookupInterval, cfg.LookupInterval)
	}
	if cfg.Source == nil {
		return nil, ErrSourceRequired
	}
	customSigils, err := cloneConfiguredSigils(cfg.Sigils)
	if err != nil {
		return nil, err
	}

	capture := common.NewAdminCapture()
	if err := cfg.Source.SetAdmin(capture); err != nil {
		return nil, fmt.Errorf("ninfo: capture admin handlers: %w", err)
	}

	nodeInfo := capture.Handlers["getNodeInfo"]
	if nodeInfo == nil {
		return nil, ErrNodeInfoNotCaptured
	}
	obj := &Obj{
		source:         cfg.Source,
		nodeInfo:       nodeInfo,
		tasks:          common.NewTaskGroup(context.Background()),
		maxAskTime:     orDefaultDuration(cfg.MaxAskTime, defaultMaxAskTime),
		askRetryPause:  orDefaultDuration(cfg.AskRetryPause, defaultAskRetryPause),
		lookupInterval: orDefaultDuration(cfg.LookupInterval, defaultLookupInterval),
		maxLookupTime:  orDefaultDuration(cfg.MaxLookupTime, defaultMaxLookupTime),
		sigils:         customSigils,
		askFlights:     make(map[[ed25519.PublicKeySize]byte]*askFlightObj),
		resolveFlights: make(map[netip.Addr]*resolveFlightObj),
	}
	return obj, nil
}

func cloneConfiguredSigils(configured []sigils.Interface) ([]sigils.Interface, error) {
	if len(configured) == 0 {
		return nil, nil
	}
	cloned := make([]sigils.Interface, 0, len(configured))
	seen := make(map[string]struct{}, len(configured))
	var errs []error
	for _, parser := range configured {
		if parser == nil {
			errs = append(errs, fmt.Errorf("%w: nil parser", ErrInvalidSigil))
			continue
		}
		name := parser.GetName()
		if !sigils.ValidateName(name) {
			errs = append(errs, fmt.Errorf("%w: invalid name %q", ErrInvalidSigil, name))
			continue
		}
		if reservedSigilName(name) {
			errs = append(errs, fmt.Errorf("%w: name %q is reserved", ErrInvalidSigil, name))
			continue
		}
		if _, exists := seen[name]; exists {
			errs = append(errs, fmt.Errorf("%w: duplicate name %q", ErrInvalidSigil, name))
			continue
		}
		clone := parser.Clone()
		if clone == nil {
			errs = append(errs, fmt.Errorf("%w: parser %q returned a nil clone", ErrInvalidSigil, name))
			continue
		}
		seen[name] = struct{}{}
		cloned = append(cloned, clone)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return cloned, nil
}

// Close releases resources held by the module.
// Close cancels shared work and waits for accepted Ask flights to leave the
// captured handler. Standalone Close intentionally waits for accepted work; the
// root ratatoskr object bounds its aggregate shutdown with ConfigObj.CloseTimeout.
func (obj *Obj) Close() error {
	obj.askMu.Lock()
	obj.closed = true
	tasks := obj.tasks
	obj.askMu.Unlock()
	if tasks == nil {
		return nil
	}
	tasks.Wait()
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
	return obj.closed || obj.tasks == nil
}

// Ask queries a remote node's NodeInfo by its public key. Concurrent callers for
// the same key share one detached flight. Canceling a caller only stops that
// caller's wait; the flight remains available to other waiters. MaxAskTime stops
// retries, but one synchronous upstream call may finish after it. Distinct flights
// are capped internally.
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
	tasks := obj.tasks
	obj.askMu.Unlock()
	if !tasks.Go(func(context.Context) { obj.runAskFlight(ka, flight) }) {
		obj.finishAskFlight(ka, flight, ErrClosed)
	}
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

func (obj *Obj) finishAskFlight(key [ed25519.PublicKeySize]byte, flight *askFlightObj, err error) {
	if err != nil {
		flight.err = err
	}
	obj.askMu.Lock()
	if obj.askFlights[key] == flight {
		delete(obj.askFlights, key)
	}
	obj.askMu.Unlock()
	close(flight.done)
}

func (obj *Obj) runAskFlight(key [ed25519.PublicKeySize]byte, flight *askFlightObj) {
	defer obj.finishAskFlight(key, flight, nil)

	workCtx := obj.tasks.Context()
	cancel := func() {}
	if obj.maxAskTime > 0 {
		workCtx, cancel = context.WithTimeout(workCtx, obj.maxAskTime)
	}
	defer cancel()
	retryPause := obj.askRetryPause
	var lastErr error
	for {
		if err := workCtx.Err(); err != nil {
			if errors.Is(err, context.Canceled) && obj.isClosed() {
				flight.err = ErrClosed
			} else if lastErr != nil {
				flight.err = lastErr
			} else {
				flight.err = err
			}
			return
		}
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
