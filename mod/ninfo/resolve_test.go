package ninfo

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
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
// parsePkYggCandidate

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
// parseHexKey

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
// extractIPv6

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
// matchYggAddr

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

// // // // // // // // // //
// resolveIPv6 context handling

func newResolveCore(t *testing.T) *coremod.Obj {
	t.Helper()
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	node, err := coremod.New(coremod.ConfigObj{
		Config:          cfg,
		CoreStopTimeout: time.Second,
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

type lookupOnlySourceObj struct {
	lookups atomic.Int64
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func (s *lookupOnlySourceObj) SetAdmin(yggcore.AddHandler) error {
	return nil
}

func (s *lookupOnlySourceObj) SendLookup(ed25519.PublicKey) {
	s.lookups.Add(1)
	if s.started != nil {
		s.once.Do(func() {
			close(s.started)
		})
	}
	if s.release != nil {
		<-s.release
	}
}

func (s *lookupOnlySourceObj) GetPeers() []yggcore.PeerInfo {
	return nil
}

func (s *lookupOnlySourceObj) GetSessions() []yggcore.SessionInfo {
	return nil
}

func (s *lookupOnlySourceObj) GetPaths() []yggcore.PathEntryInfo {
	return nil
}

func TestAskAddr_nilContextIPv6DoesNotPanic(t *testing.T) {
	coreObj := newResolveCore(t)
	obj := newTestObj()
	obj.source = coreObj
	obj.maxLookupTime = 20 * time.Millisecond
	obj.lookupInterval = time.Millisecond
	//lint:ignore SA1012 this test verifies the documented nil-context contract.
	_, err := obj.AskAddr(nil, testYggIPv6(t)) //nolint:staticcheck // Verifies the documented nil-context contract.
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

func TestAskAddr_ipv6DoesNotUseAskLimit(t *testing.T) {
	src := &lookupOnlySourceObj{}
	obj := newTestObj()
	obj.source = src
	obj.maxConcurrentAsks = 1
	obj.maxLookupTime = 100 * time.Millisecond
	obj.lookupInterval = 50 * time.Millisecond
	obj.nodeInfo = func(json.RawMessage) (interface{}, error) {
		return yggcore.GetNodeInfoResponse{
			"test": json.RawMessage(`{"name":"ratatoskr"}`),
		}, nil
	}

	firstErr := make(chan error, 1)
	go func() {
		_, err := obj.AskAddr(context.Background(), testYggIPv6(t))
		firstErr <- err
	}()

	deadline := time.After(time.Second)
	for src.lookups.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("first lookup did not start")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	result, err := obj.Ask(ctx, genKey(t))
	if err != nil {
		t.Fatalf("Ask should not wait for IPv6 lookup limit: %v", err)
	}
	if result.Node == nil {
		t.Fatal("expected Ask result node")
	}

	select {
	case err = <-firstErr:
		if !errors.Is(err, ErrUnresolvableAddr) {
			t.Fatalf("expected first lookup to finish with ErrUnresolvableAddr, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("first lookup did not finish")
	}
}

func TestAskAddr_deduplicatesConcurrentIPv6Lookup(t *testing.T) {
	src := &lookupOnlySourceObj{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	obj := newTestObj()
	obj.source = src
	obj.maxLookupTime = 20 * time.Millisecond
	obj.lookupInterval = time.Second
	addr := testYggIPv6(t)

	ready := make(chan struct{}, 8)
	start := make(chan struct{})
	done := make(chan error, 8)
	for range 8 {
		go func() {
			ready <- struct{}{}
			<-start
			_, err := obj.AskAddr(context.Background(), addr)
			done <- err
		}()
	}
	for range 8 {
		<-ready
	}
	close(start)

	select {
	case <-src.started:
	case <-time.After(time.Second):
		t.Fatal("lookup did not start")
	}
	time.Sleep(5 * time.Millisecond)
	close(src.release)

	for range 8 {
		err := <-done
		if !errors.Is(err, ErrUnresolvableAddr) {
			t.Fatalf("expected ErrUnresolvableAddr, got %v", err)
		}
	}
	if got := src.lookups.Load(); got != 1 {
		t.Fatalf("expected one shared SendLookup, got %d", got)
	}
}

func TestAskAddr_leaderDeadlineDoesNotBreakFollower(t *testing.T) {
	src := &lookupOnlySourceObj{started: make(chan struct{})}
	obj := newTestObj()
	obj.source = src
	obj.maxLookupTime = 150 * time.Millisecond
	obj.lookupInterval = time.Second
	addr := testYggIPv6(t)

	// Leader with a short deadline starts the shared, detached lookup.
	leaderCtx, leaderCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer leaderCancel()
	leaderErr := make(chan error, 1)
	go func() {
		_, err := obj.AskAddr(leaderCtx, addr)
		leaderErr <- err
	}()

	select {
	case <-src.started:
	case <-time.After(time.Second):
		t.Fatal("shared lookup did not start")
	}

	// Follower with a long deadline joins the in-flight lookup.
	followerErr := make(chan error, 1)
	go func() {
		_, err := obj.AskAddr(context.Background(), addr)
		followerErr <- err
	}()

	// Leader returns on its own short deadline.
	select {
	case err := <-leaderErr:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("leader: expected DeadlineExceeded, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("leader did not return")
	}

	// The follower must NOT be aborted by the leader's deadline: the shared
	// lookup runs under the node lifetime and finishes on its own budget, so the
	// follower sees ErrUnresolvableAddr, not the leader's context error.
	select {
	case err := <-followerErr:
		if !errors.Is(err, ErrUnresolvableAddr) {
			t.Fatalf("follower killed by leader deadline: got %v, want ErrUnresolvableAddr", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("follower did not return")
	}
}

func TestAskAddr_negativeCacheSkipsRepeatLookup(t *testing.T) {
	src := &lookupOnlySourceObj{}
	obj := newTestObj()
	obj.source = src
	obj.maxLookupTime = 30 * time.Millisecond
	obj.lookupInterval = time.Second
	addr := testYggIPv6(t)

	// First lookup polls then fails (unresolvable) → address is negatively cached.
	if _, err := obj.AskAddr(context.Background(), addr); !errors.Is(err, ErrUnresolvableAddr) {
		t.Fatalf("first: expected ErrUnresolvableAddr, got %v", err)
	}
	first := src.lookups.Load()
	if first == 0 {
		t.Fatal("expected at least one SendLookup on first attempt")
	}

	// Immediate repeat must hit the negative cache: fast, no new SendLookup flight.
	start := time.Now()
	if _, err := obj.AskAddr(context.Background(), addr); !errors.Is(err, ErrUnresolvableAddr) {
		t.Fatalf("second: expected ErrUnresolvableAddr, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Millisecond {
		t.Fatalf("repeat lookup should return from negative cache fast, took %s", elapsed)
	}
	if got := src.lookups.Load(); got != first {
		t.Fatalf("negative cache must skip re-lookup: SendLookup count %d, want %d", got, first)
	}
}

// // // // // // // // // //

func BenchmarkParseHexKey(b *testing.B) {
	h := hex.EncodeToString(make([]byte, 32))
	for b.Loop() {
		if _, err := parseHexKey(h); err != nil {
			b.Fatalf("parseHexKey: %v", err)
		}
	}
}

func BenchmarkMatchYggAddr(b *testing.B) {
	key := make(ed25519.PublicKey, 32)
	addr := yggaddr.AddrForKey(key)
	ip := net.IP(addr[:])
	for b.Loop() {
		matchYggAddr(key, ip)
	}
}
