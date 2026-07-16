// Package ninfo queries and parses Yggdrasil NodeInfo.
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

// Obj queries remote NodeInfo through a Yggdrasil source.
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
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	waiters int
	raw     json.RawMessage
	rtt     time.Duration
	err     error
}

type resolveFlightObj struct {
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	waiters int
	key     ed25519.PublicKey
	err     error
}

// ConfigObj contains NodeInfo query and parser settings.
type ConfigObj struct {
	// Source is the running core.
	Source SourceInterface

	// MaxAskTime bounds retries for one shared NodeInfo query. Zero uses 30 seconds;
	// a negative value disables the deadline.
	MaxAskTime time.Duration
	// AskRetryPause is the delay between query attempts. Zero uses 500ms; a
	// negative value disables retries.
	AskRetryPause time.Duration
	// LookupInterval is the initial address-resolution poll interval. Zero uses
	// 100ms; a negative value is invalid.
	LookupInterval time.Duration
	// MaxLookupTime bounds one shared address lookup. Zero uses 30 seconds; a
	// negative value disables the deadline.
	MaxLookupTime time.Duration

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

// New captures the source's getNodeInfo handler and creates a query module.
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

// Close rejects new work and waits for accepted shared queries.
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
	return obj.closed || obj.tasks == nil
}

// Ask queries NodeInfo by public key. Callers for the same key share work while
// retaining independent cancellation.
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

	for {
		obj.askMu.Lock()
		if obj.closedLocked() {
			obj.askMu.Unlock()
			return nil, ErrClosed
		}
		if obj.askFlights == nil {
			obj.askFlights = make(map[[ed25519.PublicKeySize]byte]*askFlightObj)
		}
		if flight := obj.askFlights[ka]; flight != nil {
			if flight.waiters == 0 {
				done := flight.done
				obj.askMu.Unlock()
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-done:
				}
				continue
			}
			flight.waiters++
			obj.askMu.Unlock()
			return obj.waitAskFlight(ctx, ka, flight)
		}
		if len(obj.askFlights) >= maxConcurrentAsks {
			obj.askMu.Unlock()
			return nil, ErrAskBusy
		}
		flightCtx, flightCancel := context.WithCancel(obj.tasks.Context())
		flight := &askFlightObj{
			ctx:     flightCtx,
			cancel:  flightCancel,
			done:    make(chan struct{}),
			waiters: 1,
		}
		obj.askFlights[ka] = flight
		tasks := obj.tasks
		obj.askMu.Unlock()
		if !tasks.Go(func(context.Context) { obj.runAskFlight(ka, flight) }) {
			obj.finishAskFlight(ka, flight, ErrClosed)
		}
		return obj.waitAskFlight(ctx, ka, flight)
	}
}

func (obj *Obj) waitAskFlight(ctx context.Context, key [ed25519.PublicKeySize]byte, flight *askFlightObj) (*AskResultObj, error) {
	defer obj.releaseAskWaiter(key, flight)
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

func (obj *Obj) releaseAskWaiter(key [ed25519.PublicKeySize]byte, flight *askFlightObj) {
	obj.askMu.Lock()
	flight.waiters--
	cancel := flight.waiters == 0 && obj.askFlights[key] == flight
	obj.askMu.Unlock()
	if cancel && flight.cancel != nil {
		flight.cancel()
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
	if flight.cancel != nil {
		flight.cancel()
	}
	close(flight.done)
}

func (obj *Obj) runAskFlight(key [ed25519.PublicKeySize]byte, flight *askFlightObj) {
	defer obj.finishAskFlight(key, flight, nil)

	workCtx := flight.ctx
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

// AskAddr resolves an address string to a public key and calls Ask.
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
