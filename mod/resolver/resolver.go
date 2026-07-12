package resolver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/voluminor/ratatoskr/internal/common"
	"github.com/yggdrasil-network/yggdrasil-go/src/address"
	"golang.org/x/net/proxy"
)

// // // // // // // // // //

const (
	defaultLookupTimeout    = 10 * time.Second
	defaultCacheTTL         = 30 * time.Second
	defaultNegativeCacheTTL = 3 * time.Second
	defaultCacheMaxEntries  = 4096
	maxDNSNameLength        = 253
	// maxConcurrentLookups bounds distinct in-flight DNS names. The resolver serves
	// untrusted SOCKS clients that can request unlimited unique hostnames, each
	// detaching a leader lookup (context.WithoutCancel) that outlives the caller;
	// without this cap a name flood exhausts goroutines and nameserver dials.
	maxConcurrentLookups = 256
)

// //

// Obj — name resolver supporting .pk.ygg and DNS over Yggdrasil
type Obj struct {
	resolver        *net.Resolver
	hasDNS          bool
	dnsErr          error
	lookupTimeout   time.Duration
	cacheTTL        time.Duration
	cacheMaxEntries int
	lookupMu        sync.Mutex
	lookupFlights   map[string]*lookupFlightObj
	cacheMu         sync.RWMutex
	cache           map[string]cacheEntryObj
}

// ConfigObj controls DNS lookup behavior.
type ConfigObj struct {
	// Dialer used for DNS traffic over Yggdrasil.
	Dialer proxy.ContextDialer
	// DNS server address. Empty string disables DNS and keeps only .pk.ygg/literals.
	Nameserver string
	// DNS lookup timeout applied per lookup: 0 -> default (10s), N>0 -> N used
	// as-is, N<0 -> disabled (bounded only by the caller's context).
	LookupTimeout time.Duration
	// Positive DNS cache TTL; 0 -> safe default, <0 -> disabled.
	CacheTTL time.Duration
	// Positive DNS cache cap; 0 -> safe default, <0 -> disabled.
	CacheMaxEntries int
}

type cacheEntryObj struct {
	ip      net.IP
	err     error
	expires time.Time
}

type lookupFlightObj struct {
	done chan struct{}
	ip   net.IP
	err  error
}

func effectiveLookupTimeout(d time.Duration) time.Duration {
	switch {
	case d == 0:
		return defaultLookupTimeout
	case d < 0:
		return 0
	default:
		return d
	}
}

func effectiveCacheTTL(d time.Duration) time.Duration {
	switch {
	case d == 0:
		return defaultCacheTTL
	case d < 0:
		return 0
	default:
		return d
	}
}

func effectiveCacheMaxEntries(n int) int {
	switch {
	case n == 0:
		return defaultCacheMaxEntries
	case n < 0:
		return 0
	default:
		return n
	}
}

func cloneIP(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	out := make(net.IP, len(ip))
	copy(out, ip)
	return out
}

func isYggdrasilIP(ip net.IP) bool {
	raw := ip.To16()
	if raw == nil {
		return false
	}
	var addr address.Address
	copy(addr[:], raw)
	return addr.IsValid()
}

func firstYggdrasilIP(addrs []net.IP) net.IP {
	for _, ip := range addrs {
		if isYggdrasilIP(ip) {
			return ip
		}
	}
	return nil
}

func safeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (r *Obj) lookupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	baseCtx := context.WithoutCancel(safeContext(ctx))
	if r.lookupTimeout > 0 {
		return context.WithTimeout(baseCtx, r.lookupTimeout)
	}
	// Disabled (lookupTimeout <= 0): no internal deadline; the lookup is bounded
	// only by whatever the caller propagates through its own context.
	return context.WithCancel(baseCtx)
}

func (r *Obj) waitLookupFlight(ctx context.Context, flight *lookupFlightObj) (net.IP, error) {
	select {
	case <-flight.done:
		if flight.err != nil {
			return nil, flight.err
		}
		return cloneIP(flight.ip), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func cacheKey(name string) string {
	return strings.ToLower(name)
}

// validDNSName rejects names DNS can never resolve so junk queries neither reach
// the resolver nor churn the negative cache: over-length names and names with an
// empty label. A single trailing dot (FQDN form) is tolerated.
func validDNSName(name string) bool {
	name = strings.TrimSuffix(name, ".")
	if len(name) == 0 || len(name) > maxDNSNameLength {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" {
			return false
		}
	}
	return true
}

func resolvePublicKeyDomain(name string) (net.IP, bool, error) {
	key, matched, err := common.ParsePublicKeyDomain(name)
	if err != nil {
		if errors.Is(err, common.ErrInvalidPublicKeyLength) {
			return nil, true, fmt.Errorf("%w: %w: %v", ErrInvalidPublicKeyDomain, ErrInvalidKeyLength, err)
		}
		return nil, true, fmt.Errorf("%w: %v", ErrInvalidPublicKeyDomain, err)
	}
	if !matched {
		return nil, false, nil
	}
	return address.AddrForKey(key)[:], true, nil
}

func (r *Obj) cacheGetDNS(key string, now time.Time) (net.IP, error, bool) {
	entry, ok := r.cacheGetEntry(key, now)
	if !ok {
		return nil, nil, false
	}
	if entry.err != nil {
		return nil, entry.err, true
	}
	return entry.ip, nil, true
}

func (r *Obj) cacheGetEntry(key string, now time.Time) (cacheEntryObj, bool) {
	r.cacheMu.RLock()
	if r.cacheTTL <= 0 || r.cache == nil {
		r.cacheMu.RUnlock()
		return cacheEntryObj{}, false
	}
	entry, ok := r.cache[key]
	if ok && now.Before(entry.expires) {
		r.cacheMu.RUnlock()
		entry.ip = cloneIP(entry.ip)
		return entry, true
	}
	r.cacheMu.RUnlock()
	if ok {
		r.cacheMu.Lock()
		if r.cache != nil {
			if entry, ok = r.cache[key]; ok && !now.Before(entry.expires) {
				delete(r.cache, key)
			}
		}
		r.cacheMu.Unlock()
	}
	return cacheEntryObj{}, false
}

func (r *Obj) cacheSet(key string, ip net.IP, now time.Time) {
	if ip == nil {
		return
	}
	r.cacheSetEntry(key, cacheEntryObj{ip: cloneIP(ip), expires: now.Add(r.cacheTTL)})
}

func (r *Obj) cacheSetError(key string, err error, now time.Time) {
	if err == nil {
		return
	}
	ttl := r.negativeCacheTTL()
	if ttl <= 0 {
		return
	}
	r.cacheSetEntry(key, cacheEntryObj{err: err, expires: now.Add(ttl)})
}

func (r *Obj) negativeCacheTTL() time.Duration {
	ttl := r.cacheTTL
	if ttl <= 0 {
		return 0
	}
	if ttl < defaultNegativeCacheTTL {
		return ttl
	}
	return defaultNegativeCacheTTL
}

func (r *Obj) cacheSetEntry(key string, entry cacheEntryObj) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	if r.cacheTTL <= 0 || r.cache == nil {
		return
	}
	// Uniform TTL: insertion order equals expiry order, so a full cache just drops
	// one entry and re-queries on the next miss; lazy expiry on read reclaims the
	// rest. No separate ordering structure is needed.
	if _, exists := r.cache[key]; !exists && len(r.cache) >= r.cacheMaxEntries {
		r.cacheEvictOneLocked()
	}
	r.cache[key] = entry
}

func (r *Obj) cacheEvictOneLocked() {
	for key := range r.cache {
		delete(r.cache, key)
		return
	}
}

func (r *Obj) lookupDNS(ctx context.Context, key, name string) (net.IP, error) {
	waitCtx := safeContext(ctx)
	select {
	case <-waitCtx.Done():
		return nil, waitCtx.Err()
	default:
	}
	// Reject junk names before any flight or cache write to avoid churn.
	if !validDNSName(name) {
		return nil, fmt.Errorf("%w for %q", ErrNoAddresses, name)
	}
	now := time.Now()
	if ip, err, ok := r.cacheGetDNS(key, now); ok {
		if err != nil {
			return nil, err
		}
		return ip, nil
	}

	r.lookupMu.Lock()
	if flight := r.lookupFlights[key]; flight != nil {
		r.lookupMu.Unlock()
		return r.waitLookupFlight(waitCtx, flight)
	}
	// Admission cap on distinct in-flight names: joining an existing flight is
	// always allowed (dedup), only a genuinely new name counts against the bound.
	if len(r.lookupFlights) >= maxConcurrentLookups {
		r.lookupMu.Unlock()
		return nil, ErrLookupBusy
	}
	flight := &lookupFlightObj{done: make(chan struct{})}
	r.lookupFlights[key] = flight
	r.lookupMu.Unlock()

	go r.runLookupFlight(ctx, key, name, flight)
	return r.waitLookupFlight(waitCtx, flight)
}

func (r *Obj) runLookupFlight(ctx context.Context, key, name string, flight *lookupFlightObj) {
	defer func() {
		r.lookupMu.Lock()
		if r.lookupFlights[key] == flight {
			delete(r.lookupFlights, key)
		}
		r.lookupMu.Unlock()
		close(flight.done)
	}()

	now := time.Now()
	if ip, err, ok := r.cacheGetDNS(key, now); ok {
		flight.ip = ip
		flight.err = err
		return
	}
	lookupCtx, cancel := r.lookupContext(ctx)
	defer cancel()
	addrs, err := r.resolver.LookupIP(lookupCtx, "ip6", name)
	finished := time.Now()
	if err != nil {
		err = fmt.Errorf("lookup %q: %w", name, err)
		r.cacheSetError(key, err, finished)
		flight.err = err
		return
	}
	if len(addrs) == 0 {
		err = fmt.Errorf("%w for %q", ErrNoAddresses, name)
		r.cacheSetError(key, err, finished)
		flight.err = err
		return
	}
	ip := firstYggdrasilIP(addrs)
	if ip == nil {
		err = fmt.Errorf("%w for %q", ErrNonYggdrasilAddress, name)
		r.cacheSetError(key, err, finished)
		flight.err = err
		return
	}
	r.cacheSet(key, ip, finished)
	flight.ip = ip
}

// //

// New creates a resolver; empty Nameserver = only .pk.ygg and literals.
func New(cfg ConfigObj) *Obj {
	cacheTTL := effectiveCacheTTL(cfg.CacheTTL)
	cacheMaxEntries := effectiveCacheMaxEntries(cfg.CacheMaxEntries)
	r := &Obj{
		resolver:        &net.Resolver{PreferGo: true},
		lookupTimeout:   effectiveLookupTimeout(cfg.LookupTimeout),
		cacheTTL:        cacheTTL,
		cacheMaxEntries: cacheMaxEntries,
		lookupFlights:   make(map[string]*lookupFlightObj),
	}
	if cfg.Nameserver != "" {
		r.hasDNS = true
		if cacheTTL > 0 && cacheMaxEntries > 0 {
			r.cache = make(map[string]cacheEntryObj)
		}
		dialer := cfg.Dialer
		if dialer == nil {
			r.dnsErr = ErrDialerRequired
		}
		host, port, err := net.SplitHostPort(cfg.Nameserver)
		if err != nil {
			host = cfg.Nameserver
			port = "53"
		}
		dnsAddr := net.JoinHostPort(host, port)
		r.resolver.Dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
			if dialer == nil {
				return nil, ErrDialerRequired
			}
			return dialer.DialContext(ctx, network, dnsAddr)
		}
	}
	return r
}

// //

// Resolve — <pubkey>.pk.ygg, IPv6 literals, DNS (when nameserver is configured)
func (r *Obj) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	// Public key → IPv6
	if ip, ok, err := resolvePublicKeyDomain(name); ok {
		if err != nil {
			return ctx, nil, err
		}
		return ctx, ip, nil
	}

	// IPv6 literal
	if ip := net.ParseIP(name); ip != nil {
		return ctx, ip, nil
	}

	// DNS — only if nameserver is configured
	if !r.hasDNS {
		return ctx, nil, fmt.Errorf("%w: cannot resolve %q", ErrNoNameserver, name)
	}
	if r.dnsErr != nil {
		return ctx, nil, r.dnsErr
	}
	// lookupDNS already serves cache hits, so no redundant pre-check here.
	// The caller ctx is returned unchanged: the exported signature is part of
	// the public API and must stay stable for existing callers.
	ip, err := r.lookupDNS(ctx, cacheKey(name), name)
	if err != nil {
		return ctx, nil, err
	}
	return ctx, ip, nil
}
