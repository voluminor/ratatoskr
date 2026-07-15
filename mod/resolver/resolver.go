// Package resolver maps public-key domains, IP literals, and DNS names to IPs.
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
	maxConcurrentLookups    = 256
)

// //

// Obj resolves names using public-key mapping, literals, and optional DNS.
type Obj struct {
	resolver        *net.Resolver
	hasDNS          bool
	lookupTimeout   time.Duration
	cacheTTL        time.Duration
	cacheMaxEntries int
	lookupMu        sync.Mutex
	lookupFlights   map[string]*lookupFlightObj
	lookupWG        sync.WaitGroup
	ctx             context.Context
	cancel          context.CancelFunc
	closeOnce       sync.Once
	closed          bool
	cacheMu         sync.RWMutex
	cache           map[string]cacheEntryObj
}

// ConfigObj controls DNS lookup behavior.
type ConfigObj struct {
	// Dialer carries DNS traffic. It is required when Nameserver is set.
	Dialer proxy.ContextDialer
	// Nameserver is the DNS endpoint. An empty value disables DNS.
	Nameserver string
	// LookupTimeout bounds a shared DNS lookup. Zero uses 10 seconds; a negative
	// value disables the resolver deadline.
	LookupTimeout time.Duration
	// CacheTTL controls positive caching. Zero uses 30 seconds; negative disables it.
	CacheTTL time.Duration
	// CacheMaxEntries bounds the cache. Zero uses 4096; negative disables it.
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
	if addr.IsValid() {
		return true
	}
	var subnet address.Subnet
	copy(subnet[:], raw)
	return subnet.IsValid()
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

func (r *Obj) lookupContext() (context.Context, context.CancelFunc) {
	baseCtx := r.ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	if r.lookupTimeout > 0 {
		return context.WithTimeout(baseCtx, r.lookupTimeout)
	}
	return baseCtx, func() {}
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
	return strings.ToLower(strings.TrimSuffix(name, "."))
}

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
	if r.closed {
		r.lookupMu.Unlock()
		return nil, ErrClosed
	}
	if flight := r.lookupFlights[key]; flight != nil {
		r.lookupMu.Unlock()
		return r.waitLookupFlight(waitCtx, flight)
	}
	if len(r.lookupFlights) >= maxConcurrentLookups {
		r.lookupMu.Unlock()
		return nil, ErrLookupBusy
	}
	flight := &lookupFlightObj{done: make(chan struct{})}
	r.lookupFlights[key] = flight
	r.lookupWG.Add(1)
	r.lookupMu.Unlock()

	go r.runLookupFlight(key, name, flight)
	return r.waitLookupFlight(waitCtx, flight)
}

func (r *Obj) runLookupFlight(key, name string, flight *lookupFlightObj) {
	defer r.lookupWG.Done()
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
	lookupCtx, cancel := r.lookupContext()
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

// New creates a resolver. An empty Nameserver enables only key domains and literals.
func New(cfg ConfigObj) (*Obj, error) {
	if cfg.Nameserver != "" && cfg.Dialer == nil {
		return nil, ErrDialerRequired
	}
	cacheTTL := effectiveCacheTTL(cfg.CacheTTL)
	cacheMaxEntries := effectiveCacheMaxEntries(cfg.CacheMaxEntries)
	ctx, cancel := context.WithCancel(context.Background())
	r := &Obj{
		resolver:        &net.Resolver{PreferGo: true},
		lookupTimeout:   effectiveLookupTimeout(cfg.LookupTimeout),
		cacheTTL:        cacheTTL,
		cacheMaxEntries: cacheMaxEntries,
		lookupFlights:   make(map[string]*lookupFlightObj),
		ctx:             ctx,
		cancel:          cancel,
	}
	if cfg.Nameserver != "" {
		r.hasDNS = true
		if cacheTTL > 0 && cacheMaxEntries > 0 {
			r.cache = make(map[string]cacheEntryObj)
		}
		host, port, err := net.SplitHostPort(cfg.Nameserver)
		if err != nil {
			host = cfg.Nameserver
			port = "53"
		}
		dnsAddr := net.JoinHostPort(host, port)
		r.resolver.Dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return cfg.Dialer.DialContext(ctx, network, dnsAddr)
		}
	}
	return r, nil
}

// Close cancels and waits for admitted DNS lookups. Repeated calls succeed.
func (r *Obj) Close() error {
	r.closeOnce.Do(func() {
		r.lookupMu.Lock()
		r.closed = true
		if r.cancel != nil {
			r.cancel()
		}
		r.lookupMu.Unlock()
		r.lookupWG.Wait()
	})
	return nil
}

func (r *Obj) isClosed() bool {
	r.lookupMu.Lock()
	closed := r.closed
	r.lookupMu.Unlock()
	return closed
}

// //

// Resolve maps a public-key domain, IP literal, or DNS name to an IP address.
func (r *Obj) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	if r.isClosed() {
		return ctx, nil, ErrClosed
	}
	if ip, ok, err := resolvePublicKeyDomain(name); ok {
		if err != nil {
			return ctx, nil, err
		}
		return ctx, ip, nil
	}

	if ip := net.ParseIP(name); ip != nil {
		return ctx, ip, nil
	}

	if !r.hasDNS {
		return ctx, nil, fmt.Errorf("%w: cannot resolve %q", ErrNoNameserver, name)
	}
	ip, err := r.lookupDNS(ctx, cacheKey(name), name)
	if err != nil {
		return ctx, nil, err
	}
	return ctx, ip, nil
}
