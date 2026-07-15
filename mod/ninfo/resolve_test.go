package ninfo

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	coremod "github.com/voluminor/ratatoskr/mod/core"
	yggaddr "github.com/yggdrasil-network/yggdrasil-go/src/address"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //
func TestRegex_hexKey(t *testing.T) {
	valid := hex.EncodeToString(make([]byte, 32))
	if !reHexKey.MatchString(valid) {
		t.Fatal("should match 64 hex chars")
	}
	if reHexKey.MatchString("abcd") {
		t.Fatal("should reject short hex")
	}
	if reHexKey.MatchString(valid + "aa") {
		t.Fatal("should reject 66 hex chars")
	}
}

func TestRegex_bracketIPv6(t *testing.T) {
	if !reBracketIPv6.MatchString("[200:abcd::1]:8080") {
		t.Fatal("should match [ip6]:port")
	}
	if reBracketIPv6.MatchString("200:abcd::1") {
		t.Fatal("should not match bare ip6")
	}
	if reBracketIPv6.MatchString("[200:abcd::1]") {
		t.Fatal("should not match [ip6] without port")
	}
}

func TestRegex_bareIPv6(t *testing.T) {
	if !reBareIPv6.MatchString("200:abcd::1") {
		t.Fatal("should match bare ip6")
	}
	if !reBareIPv6.MatchString("::1") {
		t.Fatal("should match ::1")
	}
	if reBareIPv6.MatchString("abcd") {
		t.Fatal("should reject hex without colon")
	}
}

// // // // // // // // // //

func TestParsePkYggCandidate_valid(t *testing.T) {
	key := genKey(t)
	addr := hex.EncodeToString(key) + ".pk.ygg"
	got, matched, err := parsePkYggCandidate(addr)
	if !matched {
		t.Fatal("expected candidate match")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ed25519.PublicKey(got).Equal(key) {
		t.Fatal("key mismatch")
	}
}

func TestParsePkYggCandidate_uppercaseSuffix(t *testing.T) {
	key := genKey(t)
	addr := hex.EncodeToString(key) + ".PK.YGG"
	got, matched, err := parsePkYggCandidate(addr)
	if !matched {
		t.Fatal("expected candidate match")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ed25519.PublicKey(got).Equal(key) {
		t.Fatal("key mismatch")
	}
}

func TestParsePkYggCandidate_rejectsSubdomain(t *testing.T) {
	key := genKey(t)
	addr := "subdomain." + hex.EncodeToString(key) + ".pk.ygg"
	if _, matched, err := parsePkYggCandidate(addr); !matched || !errors.Is(err, ErrInvalidKeyLength) {
		t.Fatalf("expected matched candidate with ErrInvalidKeyLength, got matched=%v err=%v", matched, err)
	}
}

// // // // // // // // // //

func TestParseHexKey_valid(t *testing.T) {
	key := genKey(t)
	got, err := parseHexKey(hex.EncodeToString(key))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ed25519.PublicKey(got).Equal(key) {
		t.Fatal("key mismatch")
	}
}

func TestParseHexKey_wrongLength(t *testing.T) {
	_, err := parseHexKey("abcd")
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestParseHexKey_invalidHex(t *testing.T) {
	bad := "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"
	_, err := parseHexKey(bad)
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

// // // // // // // // // //

func TestExtractIPv6_bracket(t *testing.T) {
	ip := extractIPv6("[200:abcd::1]:8080")
	if ip == nil {
		t.Fatal("expected non-nil IP")
	}
	if ip.String() != "200:abcd::1" {
		t.Fatalf("unexpected IP: %s", ip)
	}
}

func TestExtractIPv6_bare(t *testing.T) {
	ip := extractIPv6("200:abcd::1")
	if ip == nil {
		t.Fatal("expected non-nil IP")
	}
}

func TestExtractIPv6_invalid(t *testing.T) {
	ip := extractIPv6("not-an-ip")
	if ip != nil {
		t.Fatalf("expected nil for invalid input, got %s", ip)
	}
}

// // // // // // // // // //

func TestMatchYggAddr_match(t *testing.T) {
	key := genKey(t)
	addr := yggaddr.AddrForKey(key)
	if addr == nil {
		t.Fatal("AddrForKey returned nil")
	}
	ip := net.IP(addr[:])
	if !matchYggAddr(key, ip) {
		t.Fatal("expected match")
	}
}

func TestMatchYggAddr_noMatch(t *testing.T) {
	key1 := genKey(t)
	key2 := genKey(t)
	addr := yggaddr.AddrForKey(key2)
	ip := net.IP(addr[:])
	if matchYggAddr(key1, ip) {
		t.Fatal("expected no match for different keys")
	}
}

func TestMatchYggAddr_invalidKey(t *testing.T) {
	if matchYggAddr(nil, net.ParseIP("::1")) {
		t.Fatal("expected false for nil key")
	}
}

func TestMatchYggAddr_routableSubnetHost(t *testing.T) {
	key := genKey(t)
	subnet := yggaddr.SubnetForKey(key)
	if subnet == nil {
		t.Fatal("SubnetForKey returned nil")
	}
	raw := [16]byte{}
	copy(raw[:], subnet[:])
	raw[15] = 42
	if !matchYggAddr(key, net.IP(raw[:])) {
		t.Fatal("expected a host inside the key's /64 subnet to match")
	}
}

func TestYggLookupKeyCanonicalizesSubnetHost(t *testing.T) {
	key := genKey(t)
	subnet := yggaddr.SubnetForKey(key)
	raw := [16]byte{}
	copy(raw[:], subnet[:])
	raw[15] = 42
	partial, canonical, ok := yggLookupKey(netip.AddrFrom16(raw))
	if !ok {
		t.Fatal("routable subnet host was rejected")
	}
	if len(partial) != ed25519.PublicKeySize {
		t.Fatalf("partial key length = %d", len(partial))
	}
	want := raw
	clear(want[8:])
	if canonical != netip.AddrFrom16(want) {
		t.Fatalf("canonical subnet = %s, want %s", canonical, netip.AddrFrom16(want))
	}
}

// // // // // // // // // //

func newResolveCore(t *testing.T) *coremod.Obj {
	t.Helper()
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	node, err := coremod.New(coremod.ConfigObj{
		Config: cfg,
	})
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	t.Cleanup(func() { _ = node.Close() })
	return node
}

func testYggIPv6(t *testing.T) string {
	t.Helper()
	addr := yggaddr.AddrForKey(genKey(t))
	if addr == nil {
		t.Fatal("AddrForKey returned nil")
	}
	return net.IP(addr[:]).String()
}

type blockingResolveSourceObj struct {
	started  chan struct{}
	release  chan struct{}
	once     sync.Once
	lookups  atomic.Int64
	peers    atomic.Int64
	sessions atomic.Int64
	paths    atomic.Int64
}

type countingResolveSourceObj struct {
	lookups  atomic.Int64
	peers    atomic.Int64
	sessions atomic.Int64
	paths    atomic.Int64
	pathKey  ed25519.PublicKey
}

func (s *countingResolveSourceObj) SetAdmin(yggcore.AddHandler) error { return nil }
func (s *countingResolveSourceObj) SendLookup(ed25519.PublicKey)      { s.lookups.Add(1) }
func (s *countingResolveSourceObj) GetPeers() []yggcore.PeerInfo {
	s.peers.Add(1)
	return nil
}
func (s *countingResolveSourceObj) GetSessions() []yggcore.SessionInfo {
	s.sessions.Add(1)
	return nil
}
func (s *countingResolveSourceObj) GetPaths() []yggcore.PathEntryInfo {
	s.paths.Add(1)
	if s.pathKey == nil {
		return nil
	}
	return []yggcore.PathEntryInfo{{Key: s.pathKey}}
}

func (s *blockingResolveSourceObj) SetAdmin(yggcore.AddHandler) error { return nil }
func (s *blockingResolveSourceObj) SendLookup(ed25519.PublicKey)      { s.lookups.Add(1) }
func (s *blockingResolveSourceObj) GetPeers() []yggcore.PeerInfo {
	s.peers.Add(1)
	s.once.Do(func() { close(s.started) })
	<-s.release
	return nil
}
func (s *blockingResolveSourceObj) GetSessions() []yggcore.SessionInfo {
	s.sessions.Add(1)
	return nil
}
func (s *blockingResolveSourceObj) GetPaths() []yggcore.PathEntryInfo {
	s.paths.Add(1)
	return nil
}

func TestResolveIPv6CoalescesConcurrentCanonicalAddress(t *testing.T) {
	obj := newTestObj()
	source := &blockingResolveSourceObj{started: make(chan struct{}), release: make(chan struct{})}
	obj.source = source
	obj.lookupInterval = time.Hour
	obj.maxLookupTime = time.Hour
	addr := testYggIPv6(t)

	const callers = 32
	start := make(chan struct{})
	var entered sync.WaitGroup
	entered.Add(callers)
	results := make(chan error, callers)
	for i := range callers {
		go func(i int) {
			entered.Done()
			<-start
			query := addr
			if i%2 != 0 {
				query = "[" + addr + "]:1234"
			}
			_, err := obj.resolveIPv6(context.Background(), query)
			results <- err
		}(i)
	}
	entered.Wait()
	close(start)
	select {
	case <-source.started:
	case <-time.After(time.Second):
		t.Fatal("resolve flight did not start")
	}
	time.Sleep(20 * time.Millisecond)
	obj.askMu.Lock()
	flights := len(obj.resolveFlights)
	obj.askMu.Unlock()
	if flights != 1 {
		t.Fatalf("resolve flights = %d, want 1", flights)
	}

	close(source.release)
	deadline := time.Now().Add(time.Second)
	for source.lookups.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := source.lookups.Load(); got != 1 {
		t.Fatalf("SendLookup calls = %d, want 1 shared call", got)
	}
	if err := obj.Close(); err != nil {
		t.Fatal(err)
	}
	for range callers {
		if err := <-results; !errors.Is(err, ErrClosed) {
			t.Fatalf("waiter error = %v, want ErrClosed", err)
		}
	}
}

func TestResolveIPv6RejectsDistinctFlightBeyondCap(t *testing.T) {
	obj := newTestObj()
	obj.resolveFlights = make(map[netip.Addr]*resolveFlightObj)
	for i := 1; i <= maxConcurrentResolves; i++ {
		addr := netip.AddrFrom16([16]byte{0x02, byte(i >> 8), byte(i)})
		obj.resolveFlights[addr] = &resolveFlightObj{done: make(chan struct{})}
	}
	if _, err := obj.resolveIPv6(context.Background(), testYggIPv6(t)); !errors.Is(err, ErrResolveBusy) {
		t.Fatalf("resolve error = %v, want ErrResolveBusy", err)
	}
}

func TestAskAddr_nilContextIPv6DoesNotPanic(t *testing.T) {
	coreObj := newResolveCore(t)
	obj := newTestObj()
	obj.source = coreObj
	obj.maxLookupTime = 20 * time.Millisecond
	obj.lookupInterval = time.Millisecond
	var ctx context.Context
	_, err := obj.AskAddr(ctx, testYggIPv6(t))
	if !errors.Is(err, ErrUnresolvableAddr) {
		t.Fatalf("expected ErrUnresolvableAddr, got %v", err)
	}
}

func TestAskAddr_canceledIPv6ContextDoesNotTouchCore(t *testing.T) {
	obj := newTestObj()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := obj.AskAddr(ctx, testYggIPv6(t))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestResolveIPv6PollingScansDirectSnapshotsOnce(t *testing.T) {
	obj := newTestObj()
	source := &countingResolveSourceObj{}
	obj.source = source
	obj.lookupInterval = time.Millisecond
	obj.maxLookupTime = 20 * time.Millisecond
	_, err := obj.resolveIPv6(context.Background(), testYggIPv6(t))
	if !errors.Is(err, ErrUnresolvableAddr) {
		t.Fatalf("resolve error = %v, want ErrUnresolvableAddr", err)
	}
	if got := source.peers.Load(); got != 1 {
		t.Fatalf("GetPeers calls = %d, want 1", got)
	}
	if got := source.sessions.Load(); got != 1 {
		t.Fatalf("GetSessions calls = %d, want 1", got)
	}
	if got := source.paths.Load(); got < 2 {
		t.Fatalf("GetPaths calls = %d, want repeated polling", got)
	}
}

func TestResolveIPv6FindsRoutableSubnetInPaths(t *testing.T) {
	key := genKey(t)
	subnet := yggaddr.SubnetForKey(key)
	raw := [16]byte{}
	copy(raw[:], subnet[:])
	raw[15] = 99
	obj := newTestObj()
	obj.source = &countingResolveSourceObj{pathKey: key}
	got, err := obj.resolveIPv6(context.Background(), netip.AddrFrom16(raw).String())
	if err != nil {
		t.Fatalf("resolve subnet: %v", err)
	}
	if !got.Equal(key) {
		t.Fatal("resolved the wrong subnet key")
	}
}
