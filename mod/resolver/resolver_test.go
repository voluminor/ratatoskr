package resolver

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voluminor/ratatoskr/internal/common"
	"golang.org/x/net/proxy"
)

// // // // // // // // // //

type failDialerObj struct{}

func (failDialerObj) DialContext(_ context.Context, _, _ string) (net.Conn, error) {
	return nil, net.ErrClosed
}

var _ proxy.ContextDialer = failDialerObj{}

type countingFailDialerObj struct {
	calls atomic.Uint64
}

func (d *countingFailDialerObj) DialContext(_ context.Context, _, _ string) (net.Conn, error) {
	d.calls.Add(1)
	return nil, net.ErrClosed
}

type countingTimeoutDialerObj struct {
	calls atomic.Uint64
}

func (d *countingTimeoutDialerObj) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	d.calls.Add(1)
	<-ctx.Done()
	return nil, ctx.Err()
}

type concurrentBlockingDialerObj struct {
	active  atomic.Int64
	max     atomic.Int64
	started chan struct{}
	release chan struct{}
}

func (d *concurrentBlockingDialerObj) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	active := d.active.Add(1)
	for {
		maxActive := d.max.Load()
		if active <= maxActive || d.max.CompareAndSwap(maxActive, active) {
			break
		}
	}
	select {
	case d.started <- struct{}{}:
	default:
	}
	defer d.active.Add(-1)
	select {
	case <-d.release:
		return nil, net.ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type blockingDialerObj struct {
	started chan struct{}
	once    sync.Once
}

func (d *blockingDialerObj) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	d.once.Do(func() {
		close(d.started)
	})
	<-ctx.Done()
	return nil, ctx.Err()
}

type udpDialerObj struct{}

func (udpDialerObj) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, address)
}

type dnsServerObj struct {
	conn    *net.UDPConn
	delay   time.Duration
	ip      net.IP
	queries atomic.Uint64
	done    chan struct{}
}

// //

func generatePKHex(t *testing.T) string {
	t.Helper()
	pk, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	return hex.EncodeToString(pk)
}

func newCacheTestResolverObj(ttl time.Duration, maxEntries int) *Obj {
	return &Obj{
		cacheTTL:        ttl,
		cacheMaxEntries: maxEntries,
		cache:           make(map[string]cacheEntryObj),
	}
}

func newDNSServer(tb testing.TB, delay time.Duration) *dnsServerObj {
	return newDNSServerWithIP(tb, delay, net.ParseIP("200::1"))
}

func newDNSServerWithIP(tb testing.TB, delay time.Duration, ip net.IP) *dnsServerObj {
	tb.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		tb.Fatalf("ResolveUDPAddr: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		tb.Fatalf("ListenUDP: %v", err)
	}
	server := &dnsServerObj{
		conn:  conn,
		delay: delay,
		ip:    ip,
		done:  make(chan struct{}),
	}
	go server.serve()
	tb.Cleanup(server.close)
	return server
}

func (s *dnsServerObj) addr() string {
	return s.conn.LocalAddr().String()
}

func (s *dnsServerObj) resetQueries() {
	s.queries.Store(0)
}

func (s *dnsServerObj) queryCount() uint64 {
	return s.queries.Load()
}

func (s *dnsServerObj) close() {
	select {
	case <-s.done:
		return
	default:
		close(s.done)
		_ = s.conn.Close()
	}
}

func (s *dnsServerObj) serve() {
	buf := make([]byte, 512)
	for {
		n, addr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				continue
			}
		}
		s.queries.Add(1)
		if s.delay > 0 {
			time.Sleep(s.delay)
		}
		resp, ok := buildAAAAResponse(buf[:n], s.ip)
		if ok {
			_, _ = s.conn.WriteToUDP(resp, addr)
		}
	}
}

func buildAAAAResponse(query []byte, ip net.IP) ([]byte, bool) {
	if len(query) < 12 {
		return nil, false
	}
	qEnd := dnsQuestionEnd(query)
	if qEnd < 0 {
		return nil, false
	}
	ip = ip.To16()
	if ip == nil {
		return nil, false
	}
	resp := make([]byte, 0, qEnd+28)
	resp = append(resp, query[0], query[1])
	resp = append(resp, 0x81, 0x80)
	resp = append(resp, 0x00, 0x01)
	resp = append(resp, 0x00, 0x01)
	resp = append(resp, 0x00, 0x00)
	resp = append(resp, 0x00, 0x00)
	resp = append(resp, query[12:qEnd]...)
	resp = append(resp, 0xc0, 0x0c)
	resp = append(resp, 0x00, 0x1c)
	resp = append(resp, 0x00, 0x01)
	resp = append(resp, 0x00, 0x00, 0x00, 0x00)
	resp = append(resp, 0x00, 0x10)
	resp = append(resp, ip...)
	return resp, true
}

func dnsQuestionEnd(query []byte) int {
	off := 12
	for {
		if off >= len(query) {
			return -1
		}
		labelLen := int(query[off])
		off++
		if labelLen == 0 {
			break
		}
		if labelLen&0xc0 != 0 || off+labelLen > len(query) {
			return -1
		}
		off += labelLen
	}
	if off+4 > len(query) {
		return -1
	}
	return off + 4
}

func resetResolverCache(r *Obj) {
	r.cacheMu.Lock()
	clear(r.cache)
	r.cacheMu.Unlock()
}

func newTestResolverObj(dialer proxy.ContextDialer, nameserver string, cfg ConfigObj) *Obj {
	cfg.Dialer = dialer
	cfg.Nameserver = nameserver
	r, err := New(cfg)
	if err != nil {
		panic(err)
	}
	return r
}

func TestCacheKeyNormalizesTrailingDot(t *testing.T) {
	if a, b := cacheKey("Example.PK.YGG."), cacheKey("example.pk.ygg"); a != b {
		t.Fatalf("cache keys differ: %q != %q", a, b)
	}
}

func TestResolveAfterClose(t *testing.T) {
	r, err := New(ConfigObj{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Resolve(context.Background(), "200::1"); !errors.Is(err, ErrClosed) {
		t.Fatalf("Resolve error = %v, want ErrClosed", err)
	}
}

func resolveConcurrently(tb testing.TB, r *Obj, name string, concurrency int) {
	tb.Helper()
	var wg sync.WaitGroup
	var failures atomic.Int64
	ctx := context.Background()
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ip, err := r.Resolve(ctx, name)
			if err != nil || ip == nil {
				failures.Add(1)
			}
		}()
	}
	wg.Wait()
	if failures.Load() != 0 {
		tb.Fatalf("Resolve failed in %d goroutines", failures.Load())
	}
}

func waitForQueries(tb testing.TB, server *dnsServerObj, want uint64) {
	tb.Helper()
	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if server.queryCount() >= want {
			return
		}
		select {
		case <-deadline:
			tb.Fatalf("timed out waiting for %d DNS queries, got %d", want, server.queryCount())
		case <-ticker.C:
		}
	}
}

// //

func TestResolve_pkYgg(t *testing.T) {
	r := newTestResolverObj(failDialerObj{}, "", ConfigObj{})
	name := generatePKHex(t) + common.PublicKeyDomainSuffix
	_, ip, err := r.Resolve(context.Background(), name)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ip) < 16 {
		t.Errorf("expected IPv6 address, got %v", ip)
	}
}

func TestResolve_pkYgg_subdomain(t *testing.T) {
	r := newTestResolverObj(failDialerObj{}, "", ConfigObj{})
	pkHex := generatePKHex(t)
	name := "subdomain." + pkHex + common.PublicKeyDomainSuffix
	_, _, err := r.Resolve(context.Background(), name)
	if err == nil {
		t.Fatal("expected subdomain .pk.ygg name to be rejected")
	}
}

func TestResolve_pkYgg_invalidHex(t *testing.T) {
	r := newTestResolverObj(failDialerObj{}, "", ConfigObj{})
	name := "zz" + hex.EncodeToString(make([]byte, 31)) + common.PublicKeyDomainSuffix
	_, _, err := r.Resolve(context.Background(), name)
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
	if !errors.Is(err, ErrInvalidPublicKeyDomain) {
		t.Errorf("expected ErrInvalidPublicKeyDomain, got: %v", err)
	}
}

func TestResolve_pkYgg_shortKey(t *testing.T) {
	r := newTestResolverObj(failDialerObj{}, "", ConfigObj{})
	name := hex.EncodeToString(make([]byte, 16)) + common.PublicKeyDomainSuffix
	_, _, err := r.Resolve(context.Background(), name)
	if err == nil {
		t.Fatal("expected error for too-short key")
	}
	if !errors.Is(err, ErrInvalidKeyLength) {
		t.Errorf("expected ErrInvalidKeyLength, got: %v", err)
	}
}

func TestResolve_pkYgg_longKey(t *testing.T) {
	r := newTestResolverObj(failDialerObj{}, "", ConfigObj{})
	name := hex.EncodeToString(make([]byte, 64)) + common.PublicKeyDomainSuffix
	_, _, err := r.Resolve(context.Background(), name)
	if err == nil {
		t.Fatal("expected error for too-long key")
	}
}

func TestResolve_ipv6Literal(t *testing.T) {
	r := newTestResolverObj(failDialerObj{}, "", ConfigObj{})
	_, ip, err := r.Resolve(context.Background(), "200::1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip.String() != "200::1" {
		t.Errorf("expected 200::1, got %s", ip)
	}
}

func TestResolve_ipv4Literal(t *testing.T) {
	r := newTestResolverObj(failDialerObj{}, "", ConfigObj{})
	_, ip, err := r.Resolve(context.Background(), "192.168.1.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip == nil {
		t.Fatal("expected non-nil IP")
	}
}

func TestResolve_noDNS_hostname(t *testing.T) {
	r := newTestResolverObj(failDialerObj{}, "", ConfigObj{})
	_, _, err := r.Resolve(context.Background(), "example.com")
	if err == nil {
		t.Fatal("expected error: no nameserver configured")
	}
	if !errors.Is(err, ErrNoNameserver) {
		t.Errorf("expected ErrNoNameserver, got: %v", err)
	}
}

func TestResolve_dnsRequiresDialer(t *testing.T) {
	r, err := New(ConfigObj{
		Nameserver:    "[200::1]:53",
		LookupTimeout: 10 * time.Millisecond,
		CacheTTL:      -1,
	})
	if !errors.Is(err, ErrDialerRequired) {
		t.Fatalf("New error = %v, want ErrDialerRequired", err)
	}
	if r != nil {
		t.Fatal("New returned an object with a missing DNS dialer")
	}
}

func TestNew_hasDNS_withNameserver(t *testing.T) {
	r := newTestResolverObj(failDialerObj{}, "[200::1]:53", ConfigObj{})
	if !r.hasDNS {
		t.Error("expected hasDNS=true")
	}
}

func TestNew_hasDNS_noNameserver(t *testing.T) {
	r := newTestResolverObj(failDialerObj{}, "", ConfigObj{})
	if r.hasDNS {
		t.Error("expected hasDNS=false")
	}
}

func TestNew_defaultConfig(t *testing.T) {
	r := newTestResolverObj(failDialerObj{}, "[200::1]:53", ConfigObj{})
	if r.lookupTimeout != defaultLookupTimeout {
		t.Fatalf("expected default lookup timeout %s, got %s", defaultLookupTimeout, r.lookupTimeout)
	}
	if r.cacheTTL != defaultCacheTTL {
		t.Fatalf("expected default cache TTL %s, got %s", defaultCacheTTL, r.cacheTTL)
	}
	if r.cacheMaxEntries != defaultCacheMaxEntries {
		t.Fatalf("expected default cache cap %d, got %d", defaultCacheMaxEntries, r.cacheMaxEntries)
	}
	if r.cache == nil {
		t.Fatal("expected DNS cache to be enabled by default")
	}
}

func TestResolve_dnsLookupTimeout(t *testing.T) {
	dialer := &blockingDialerObj{started: make(chan struct{})}
	r := newTestResolverObj(dialer, "[200::1]:53", ConfigObj{
		LookupTimeout: 50 * time.Millisecond,
		CacheTTL:      -1,
	})
	start := time.Now()
	_, _, err := r.Resolve(context.Background(), "example.com")
	if err == nil {
		t.Fatal("expected lookup timeout error")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("lookup was not bounded by LookupTimeout, elapsed=%s", elapsed)
	}
	select {
	case <-dialer.started:
	default:
		t.Fatal("dialer was not called")
	}
}

func TestResolve_dnsRejectsNonYggdrasilAddress(t *testing.T) {
	server := newDNSServerWithIP(t, 0, net.ParseIP("::1"))
	r := newTestResolverObj(udpDialerObj{}, server.addr(), ConfigObj{
		LookupTimeout: time.Second,
		CacheTTL:      -1,
	})
	_, _, err := r.Resolve(context.Background(), "example.com")
	if !errors.Is(err, ErrNonYggdrasilAddress) {
		t.Fatalf("expected ErrNonYggdrasilAddress, got %v", err)
	}
}

func TestResolve_dnsAcceptsYggdrasilSubnetAddress(t *testing.T) {
	server := newDNSServerWithIP(t, 0, net.ParseIP("300::1"))
	r := newTestResolverObj(udpDialerObj{}, server.addr(), ConfigObj{
		LookupTimeout: time.Second,
		CacheTTL:      -1,
	})
	_, ip, err := r.Resolve(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("resolve subnet address: %v", err)
	}
	if got := ip.String(); got != "300::1" {
		t.Fatalf("resolved address = %s, want 300::1", got)
	}
}

func TestResolve_dnsWaiterContextCancel(t *testing.T) {
	dialer := &blockingDialerObj{started: make(chan struct{})}
	r := newTestResolverObj(dialer, "[200::1]:53", ConfigObj{
		LookupTimeout: 200 * time.Millisecond,
		CacheTTL:      -1,
	})
	done := make(chan error, 1)
	go func() {
		_, _, err := r.Resolve(context.Background(), "example.com")
		done <- err
	}()
	<-dialer.started

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err := r.Resolve(ctx, "example.com")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("waiter ignored its context deadline, elapsed=%s", elapsed)
	}
	if err = <-done; err == nil {
		t.Fatal("expected first lookup to finish with timeout")
	}
}

func TestResolve_dnsLeaderCancelDoesNotPoisonWaiterWithoutLookupTimeout(t *testing.T) {
	server := newDNSServer(t, 50*time.Millisecond)
	r := newTestResolverObj(udpDialerObj{}, server.addr(), ConfigObj{
		LookupTimeout: -1,
		CacheTTL:      -1,
	})

	leaderCtx, leaderCancel := context.WithCancel(context.Background())
	leaderErr := make(chan error, 1)
	go func() {
		_, _, err := r.Resolve(leaderCtx, "shared.example")
		leaderErr <- err
	}()
	waitForQueries(t, server, 1)
	leaderCancel()

	_, ip, err := r.Resolve(context.Background(), "shared.example")
	if err != nil {
		t.Fatalf("waiter should receive shared DNS result, got %v", err)
	}
	if ip == nil || ip.String() != "200::1" {
		t.Fatalf("unexpected waiter IP: %v", ip)
	}
	if err := <-leaderErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected leader context cancellation, got %v", err)
	}
}

func TestLookupContext_disabledTimeoutHasNoInternalDeadline(t *testing.T) {
	r := newTestResolverObj(&countingFailDialerObj{}, "[200::1]:53", ConfigObj{
		LookupTimeout: -1,
	})
	ctx, cancel := r.lookupContext()
	defer cancel()
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("disabled lookup timeout must not impose an internal deadline")
	}
}

func TestCache_getSetExpireAndCap(t *testing.T) {
	r := newCacheTestResolverObj(time.Minute, 2)
	now := time.Now()
	r.cacheSet("a.example", net.ParseIP("200::1"), now)
	_, _, ok := r.cacheGetDNS("a.example", now)
	if !ok {
		t.Fatal("expected cache hit")
	}

	r.cacheSet("b.example", net.ParseIP("200::2"), now)
	r.cacheSet("c.example", net.ParseIP("200::3"), now)
	if got := len(r.cache); got > r.cacheMaxEntries {
		t.Fatalf("expected cache cap %d, got %d", r.cacheMaxEntries, got)
	}

	if _, _, ok = r.cacheGetDNS("a.example", now.Add(2*time.Minute)); ok {
		t.Fatal("expected expired cache entry to miss")
	}
}

func TestCache_evictsWhenFull(t *testing.T) {
	r := newCacheTestResolverObj(time.Minute, 2)
	now := time.Now()
	r.cacheSetEntry("old", cacheEntryObj{ip: net.ParseIP("200::1"), expires: now.Add(time.Second)})
	r.cacheSetEntry("new", cacheEntryObj{ip: net.ParseIP("200::2"), expires: now.Add(2 * time.Second)})
	r.cacheSetEntry("third", cacheEntryObj{ip: net.ParseIP("200::3"), expires: now.Add(3 * time.Second)})

	r.cacheMu.RLock()
	size := len(r.cache)
	_, thirdExists := r.cache["third"]
	r.cacheMu.RUnlock()
	if size > r.cacheMaxEntries {
		t.Fatalf("cache exceeded cap: size=%d cap=%d", size, r.cacheMaxEntries)
	}
	if !thirdExists {
		t.Fatal("freshly inserted entry should be present after eviction")
	}
}

func TestResolve_dnsNegativeCache(t *testing.T) {
	dialer := &countingFailDialerObj{}
	r := newTestResolverObj(dialer, "[200::1]:53", ConfigObj{
		LookupTimeout:   time.Second,
		CacheTTL:        time.Minute,
		CacheMaxEntries: 8,
	})

	_, _, err := r.Resolve(context.Background(), "down.example")
	if err == nil {
		t.Fatal("first resolve unexpectedly succeeded")
	}
	firstCalls := dialer.calls.Load()
	if firstCalls == 0 {
		t.Fatal("first resolve did not dial")
	}
	_, _, err = r.Resolve(context.Background(), "down.example")
	if err == nil {
		t.Fatal("second resolve unexpectedly succeeded")
	}
	if got := dialer.calls.Load(); got != firstCalls {
		t.Fatalf("negative cache should suppress repeated dial, got %d dials after %d", got, firstCalls)
	}
}

func TestResolve_dnsNegativeCacheAfterLookupTimeout(t *testing.T) {
	dialer := &countingTimeoutDialerObj{}
	const name = "timeout.example"
	lookupTimeout := 120 * time.Millisecond
	cacheTTL := 60 * time.Millisecond
	r := newTestResolverObj(dialer, "[200::1]:53", ConfigObj{
		LookupTimeout:   lookupTimeout,
		CacheTTL:        cacheTTL,
		CacheMaxEntries: 8,
	})

	started := time.Now()
	_, _, err := r.Resolve(context.Background(), name)
	if err == nil {
		t.Fatal("first resolve unexpectedly succeeded")
	}
	firstCalls := dialer.calls.Load()
	if firstCalls == 0 {
		t.Fatal("first resolve did not dial")
	}
	r.cacheMu.RLock()
	entry, ok := r.cache[cacheKey(name)]
	r.cacheMu.RUnlock()
	if !ok || entry.err == nil {
		t.Fatalf("expected cached timeout error, ok=%v err=%v", ok, entry.err)
	}
	if !entry.expires.After(started.Add(lookupTimeout)) {
		t.Fatalf("negative cache expiry was based on lookup start: expires=%s started=%s timeout=%s", entry.expires, started, lookupTimeout)
	}
}

func TestResolve_dnsSameNameWaiterJoinsFlight(t *testing.T) {
	dialer := &concurrentBlockingDialerObj{
		started: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
	r := newTestResolverObj(dialer, "[200::1]:53", ConfigObj{
		LookupTimeout: time.Second,
		CacheTTL:      -1,
	})

	firstDone := make(chan error, 1)
	go func() {
		_, _, err := r.Resolve(context.Background(), "shared-limit.example")
		firstDone <- err
	}()
	select {
	case <-dialer.started:
	case <-time.After(time.Second):
		t.Fatal("first lookup did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, _, err := r.Resolve(ctx, "shared-limit.example")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiter should time out from its own wait context, got %v", err)
	}
	if got := dialer.max.Load(); got != 1 {
		t.Fatalf("same-name waiter should not start a second lookup, max active=%d", got)
	}

	close(dialer.release)
	if err := <-firstDone; err == nil {
		t.Fatal("first lookup unexpectedly succeeded")
	}
}

func TestResolve_dnsResultMutationDoesNotPoisonCache(t *testing.T) {
	server := newDNSServer(t, 0)
	r := newTestResolverObj(udpDialerObj{}, server.addr(), ConfigObj{
		LookupTimeout:   time.Second,
		CacheTTL:        time.Minute,
		CacheMaxEntries: 8,
	})
	_, ip, err := r.Resolve(context.Background(), "mutable.example")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ip == nil {
		t.Fatal("expected initial IP")
	}
	_, cached, err := r.Resolve(context.Background(), "mutable.example")
	if err != nil {
		t.Fatalf("cached Resolve: %v", err)
	}
	cached[0] = 0xff
	_, again, err := r.Resolve(context.Background(), "mutable.example")
	if err != nil {
		t.Fatalf("second cached Resolve: %v", err)
	}
	if again[0] == 0xff {
		t.Fatal("mutating returned DNS IP poisoned resolver cache")
	}
}

func TestCache_updateExistingDoesNotEvict(t *testing.T) {
	r := newCacheTestResolverObj(time.Minute, 2)
	now := time.Now()
	r.cacheSet("a.example", net.ParseIP("200::1"), now)
	r.cacheSet("b.example", net.ParseIP("200::2"), now)
	r.cacheSet("a.example", net.ParseIP("200::3"), now)

	ip, _, ok := r.cacheGetDNS("a.example", now)
	if !ok || ip.String() != "200::3" {
		t.Fatalf("expected updated a.example, ok=%v ip=%v", ok, ip)
	}
	if _, _, ok := r.cacheGetDNS("b.example", now); !ok {
		t.Fatal("updating existing key should not evict b.example")
	}
}

func TestCache_concurrentAccess(t *testing.T) {
	r := newCacheTestResolverObj(time.Minute, 64)
	now := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 512; j++ {
				key := "host-" + string(rune('a'+(id+j)%16))
				r.cacheSet(key, net.ParseIP("200::1"), now)
				_, _, _ = r.cacheGetDNS(key, now)
			}
		}(i)
	}
	wg.Wait()
	if got := len(r.cache); got > r.cacheMaxEntries {
		t.Fatalf("expected cache cap %d, got %d", r.cacheMaxEntries, got)
	}
}

func TestResolve_pkYgg_sameKeyDeterministic(t *testing.T) {
	r := newTestResolverObj(failDialerObj{}, "", ConfigObj{})
	pkHex := generatePKHex(t)
	name := pkHex + common.PublicKeyDomainSuffix
	_, ip1, err := r.Resolve(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	_, ip2, err := r.Resolve(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	if ip1.String() != ip2.String() {
		t.Errorf("same key produced different IPs: %s vs %s", ip1, ip2)
	}
}

// //

func TestResolvePublicKeyDomain_exactLength(t *testing.T) {
	pk, _, _ := ed25519.GenerateKey(rand.Reader)
	ip, matched, err := resolvePublicKeyDomain(hex.EncodeToString(pk) + common.PublicKeyDomainSuffix)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Fatal("expected .pk.ygg match")
	}
	if len(ip) < 16 {
		t.Error("expected IPv6 address")
	}
}

func TestResolvePublicKeyDomain_invalidHex(t *testing.T) {
	_, matched, err := resolvePublicKeyDomain("zz" + hex.EncodeToString(make([]byte, 31)) + common.PublicKeyDomainSuffix)
	if !matched {
		t.Fatal("expected .pk.ygg match")
	}
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidPublicKeyDomain) {
		t.Fatalf("expected ErrInvalidPublicKeyDomain, got %v", err)
	}
}

func TestResolvePublicKeyDomain_tooShort(t *testing.T) {
	_, matched, err := resolvePublicKeyDomain(hex.EncodeToString(make([]byte, 10)) + common.PublicKeyDomainSuffix)
	if !matched {
		t.Fatal("expected .pk.ygg match")
	}
	if err == nil {
		t.Fatal("expected error for short key")
	}
	if !errors.Is(err, ErrInvalidKeyLength) {
		t.Fatalf("expected ErrInvalidKeyLength, got %v", err)
	}
}

func TestResolvePublicKeyDomain_tooLong(t *testing.T) {
	_, matched, err := resolvePublicKeyDomain(hex.EncodeToString(make([]byte, 64)) + common.PublicKeyDomainSuffix)
	if !matched {
		t.Fatal("expected .pk.ygg match")
	}
	if err == nil {
		t.Fatal("expected error for long key")
	}
	if !errors.Is(err, ErrInvalidKeyLength) {
		t.Fatalf("expected ErrInvalidKeyLength, got %v", err)
	}
}

func TestResolvePublicKeyDomain_subdomainRejected(t *testing.T) {
	pk, _, _ := ed25519.GenerateKey(rand.Reader)
	pkHex := hex.EncodeToString(pk)
	_, matched, err := resolvePublicKeyDomain("some.subdomain." + pkHex + common.PublicKeyDomainSuffix)
	if !matched {
		t.Fatal("expected .pk.ygg match")
	}
	if err == nil {
		t.Fatal("expected subdomain to be rejected")
	}
}

// //

func BenchmarkResolveDNSColdHerd(b *testing.B) {
	const concurrency = 64
	server := newDNSServer(b, 200*time.Microsecond)
	r := newTestResolverObj(udpDialerObj{}, server.addr(), ConfigObj{
		LookupTimeout:   time.Second,
		CacheTTL:        time.Minute,
		CacheMaxEntries: 1024,
	})
	server.resetQueries()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		resetResolverCache(r)
		resolveConcurrently(b, r, "herd.example", concurrency)
	}
	b.StopTimer()
	b.ReportMetric(float64(server.queryCount())/float64(b.N), "dns_queries/op")
}
