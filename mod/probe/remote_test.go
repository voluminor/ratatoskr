package probe

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voluminor/ratatoskr/internal/common"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //
// parseRemotePeersResponse

func newRemoteTestObj(handler yggcore.AddHandlerFunc) *Obj {
	return &Obj{
		logger:        noopLoggerObj{},
		tasks:         common.NewTaskGroup(context.Background()),
		remoteSem:     make(chan struct{}, DefaultMaxConcurrency),
		remoteFlights: make(map[[ed25519.PublicKeySize]byte]*remoteFlightObj),
		remotePeers:   handler,
	}
}

type observedContextObj struct {
	context.Context
	observed chan<- struct{}
	once     sync.Once
}

type constructorSourceObj struct {
	treeSourceObj
}

func (s *constructorSourceObj) SetAdmin(add yggcore.AddHandler) error {
	return add.AddHandler("debug_remoteGetPeers", "", nil, func(json.RawMessage) (interface{}, error) {
		return yggcore.DebugGetPeersResponse{}, nil
	})
}

func (c *observedContextObj) Err() error {
	err := c.Context.Err()
	c.once.Do(func() { c.observed <- struct{}{} })
	return err
}

func TestNewRequiresSource(t *testing.T) {
	if _, err := New(ConfigObj{}); !errors.Is(err, ErrSourceRequired) {
		t.Fatalf("New error = %v, want ErrSourceRequired", err)
	}
}

func TestNewReturnsCloser(t *testing.T) {
	obj, err := New(ConfigObj{Source: &constructorSourceObj{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err = obj.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err = obj.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestParseRemotePeersResponse_valid(t *testing.T) {
	keys := genKeyN(t, 2)
	inner, _ := json.Marshal(struct {
		Keys []string `json:"keys"`
	}{
		Keys: []string{
			hex.EncodeToString(keys[0]),
			hex.EncodeToString(keys[1]),
		},
	})
	resp := yggcore.DebugGetPeersResponse{
		"node1": json.RawMessage(inner),
	}
	peers, _, err := parseRemotePeersResponse(resp, DefaultMaxPeersPerNode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_wrongType(t *testing.T) {
	_, _, err := parseRemotePeersResponse("not a map", DefaultMaxPeersPerNode)
	if err == nil {
		t.Fatal("expected error for wrong type")
	}
}

func TestParseRemotePeersResponse_invalidHex(t *testing.T) {
	inner, _ := json.Marshal(struct {
		Keys []string `json:"keys"`
	}{Keys: []string{"zzzz_not_hex"}})
	resp := yggcore.DebugGetPeersResponse{"n": json.RawMessage(inner)}
	peers, _, err := parseRemotePeersResponse(resp, DefaultMaxPeersPerNode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers for invalid hex, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_wrongKeyLength(t *testing.T) {
	inner, _ := json.Marshal(struct {
		Keys []string `json:"keys"`
	}{Keys: []string{hex.EncodeToString(make([]byte, 10))}})
	resp := yggcore.DebugGetPeersResponse{"n": json.RawMessage(inner)}
	peers, _, _ := parseRemotePeersResponse(resp, DefaultMaxPeersPerNode)
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers for wrong key length, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_empty(t *testing.T) {
	resp := yggcore.DebugGetPeersResponse{}
	peers, _, err := parseRemotePeersResponse(resp, DefaultMaxPeersPerNode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_nonRawMessage(t *testing.T) {
	resp := yggcore.DebugGetPeersResponse{"n": "string value"}
	peers, _, err := parseRemotePeersResponse(resp, DefaultMaxPeersPerNode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_truncatesOverLimit(t *testing.T) {
	keys := genKeyN(t, 3)
	inner, _ := json.Marshal(struct {
		Keys []string `json:"keys"`
	}{
		Keys: []string{
			hex.EncodeToString(keys[0]),
			hex.EncodeToString(keys[1]),
			hex.EncodeToString(keys[2]),
		},
	})
	resp := yggcore.DebugGetPeersResponse{
		"node1": json.RawMessage(inner),
	}
	peers, truncated, err := parseRemotePeersResponse(resp, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !truncated {
		t.Fatal("expected truncated flag for over-limit peer set")
	}
	if len(peers) != 2 {
		t.Fatalf("expected 2 peers after truncation, got %d", len(peers))
	}
}

func TestParseRemotePeersResponse_rejectsOversizedMessage(t *testing.T) {
	resp := yggcore.DebugGetPeersResponse{
		"node1": json.RawMessage(`{"keys":["` + string(make([]byte, maxRemotePeerMessageBytes)) + `"]}`),
	}
	_, _, err := parseRemotePeersResponse(resp, DefaultMaxPeersPerNode)
	if !errors.Is(err, ErrRemoteResponseTooLarge) {
		t.Fatalf("expected ErrRemoteResponseTooLarge, got %v", err)
	}
}

func TestParseRemotePeersResponse_invalidKeysDoNotConsumeLimit(t *testing.T) {
	valid := genKeyN(t, 2)
	inner, _ := json.Marshal(struct {
		Keys []string `json:"keys"`
	}{Keys: []string{"bad-1", hex.EncodeToString(valid[0]), "bad-2", hex.EncodeToString(valid[1])}})
	resp := yggcore.DebugGetPeersResponse{"node1": json.RawMessage(inner)}
	peers, truncated, err := parseRemotePeersResponse(resp, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if truncated {
		t.Fatal("valid keys within the cap must not truncate")
	}
	if len(peers) != 2 {
		t.Fatalf("expected 2 valid peers, got %d", len(peers))
	}
}

func TestCallRemotePeers_detachedCallSurvivesCallerCancel(t *testing.T) {
	key := genKey(t)
	peer := genKey(t)
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	inner, _ := json.Marshal(struct {
		Keys []string `json:"keys"`
	}{Keys: []string{hex.EncodeToString(peer)}})
	obj := newRemoteTestObj(func(json.RawMessage) (interface{}, error) {
		close(started)
		<-release
		defer close(finished)
		return yggcore.DebugGetPeersResponse{"node": json.RawMessage(inner)}, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, _, err := obj.callRemotePeers(ctx, key)
		errCh <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("remote handler did not start")
	}
	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected caller cancellation, got %v", err)
	}
	// The detached call keeps running to completion after the caller left.
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("detached remote call did not finish after caller cancel")
	}
}

func TestCallRemotePeers_closeWaitsForDetachedCall(t *testing.T) {
	key := genKey(t)
	started := make(chan struct{})
	release := make(chan struct{})
	obj := newRemoteTestObj(func(json.RawMessage) (interface{}, error) {
		close(started)
		<-release
		return yggcore.DebugGetPeersResponse{}, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, _, err := obj.callRemotePeers(ctx, key)
		errCh <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("remote handler did not start")
	}
	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		close(release)
		t.Fatalf("expected caller cancellation, got %v", err)
	}

	closed := make(chan struct{})
	go func() {
		_ = obj.Close()
		close(closed)
	}()
	select {
	case <-closed:
		close(release)
		t.Fatal("Close returned before detached call finished")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not return after detached call finished")
	}
}

func TestCloseContextTimesOutWithoutAbandoningAcceptedCall(t *testing.T) {
	key := genKey(t)
	started := make(chan struct{})
	release := make(chan struct{})
	obj := newRemoteTestObj(func(json.RawMessage) (interface{}, error) {
		close(started)
		<-release
		return yggcore.DebugGetPeersResponse{}, nil
	})

	callerCtx, cancelCaller := context.WithCancel(context.Background())
	callerDone := make(chan error, 1)
	go func() {
		_, _, err := obj.callRemotePeers(callerCtx, key)
		callerDone <- err
	}()
	<-started
	cancelCaller()
	if err := <-callerDone; !errors.Is(err, context.Canceled) {
		close(release)
		t.Fatalf("caller error = %v, want context.Canceled", err)
	}

	closeCtx, cancelClose := context.WithTimeout(context.Background(), 20*time.Millisecond)
	err := obj.CloseContext(closeCtx)
	cancelClose()
	if !errors.Is(err, context.DeadlineExceeded) {
		close(release)
		t.Fatalf("CloseContext error = %v, want context.DeadlineExceeded", err)
	}
	if _, _, err = obj.callRemotePeers(context.Background(), key); !errors.Is(err, ErrClosed) {
		close(release)
		t.Fatalf("call after CloseContext error = %v, want ErrClosed", err)
	}

	closed := make(chan struct{})
	go func() {
		_ = obj.Close()
		close(closed)
	}()
	select {
	case <-closed:
		close(release)
		t.Fatal("Close returned before the accepted call finished")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after the accepted call returned")
	}
}

func TestCloseContextReturnsSuccessWhenAlreadyClosed(t *testing.T) {
	obj := &Obj{}
	_ = obj.Close()
	if obj.tasks != nil {
		t.Fatal("zero-value Close should not initialize lifecycle state")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := obj.CloseContext(ctx); err != nil {
		t.Fatalf("CloseContext after completed Close = %v, want nil", err)
	}
}

func TestCallRemotePeersWithoutLifecycleReturnsClosed(t *testing.T) {
	obj := &Obj{
		logger:    noopLoggerObj{},
		remoteSem: make(chan struct{}, 1),
		remotePeers: func(json.RawMessage) (interface{}, error) {
			return yggcore.DebugGetPeersResponse{}, nil
		},
	}
	_, _, err := obj.callRemotePeers(context.Background(), genKey(t))
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("call error = %v, want ErrClosed", err)
	}
	if obj.tasks != nil || obj.remoteFlights != nil {
		t.Fatal("call should not repair missing lifecycle state")
	}
}

func TestCallRemotePeers_sameKeySingleFlight(t *testing.T) {
	key := genKey(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int64
	obj := &Obj{
		logger:        noopLoggerObj{},
		tasks:         common.NewTaskGroup(context.Background()),
		remoteSem:     make(chan struct{}, 4),
		remoteFlights: make(map[[ed25519.PublicKeySize]byte]*remoteFlightObj),
		remotePeers: func(json.RawMessage) (interface{}, error) {
			if calls.Add(1) == 1 {
				close(started)
			}
			<-release
			return yggcore.DebugGetPeersResponse{}, nil
		},
	}
	firstCtx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, _, err := obj.callRemotePeers(firstCtx, key)
		firstDone <- err
	}()
	<-started
	obj.remoteMu.RLock()
	flight := obj.remoteFlights[toKeyArray(key)]
	obj.remoteMu.RUnlock()
	if flight == nil {
		t.Fatal("in-flight call was not published")
	}
	secondBaseCtx, secondCancel := context.WithCancel(context.Background())
	secondObserved := make(chan struct{}, 1)
	secondDone := make(chan error, 1)
	go func() {
		secondCtx := &observedContextObj{Context: secondBaseCtx, observed: secondObserved}
		_, _, err := obj.callRemotePeers(secondCtx, key)
		secondDone <- err
	}()
	<-secondObserved
	secondCancel()
	if err := <-secondDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("second caller error = %v, want context.Canceled", err)
	}
	cancel()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("first caller error = %v", err)
	}
	close(release)
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if _, _, err := waitRemoteFlight(waitCtx, flight); err != nil {
		t.Fatalf("shared flight: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("underlying calls = %d, want 1", got)
	}
	_ = obj.Close()
}

func TestCallRemotePeers_sameKeySingleFlightWhileSemaphoreSaturated(t *testing.T) {
	key := genKey(t)
	sem := make(chan struct{}, 1)
	sem <- struct{}{}
	var calls atomic.Int64
	obj := newRemoteTestObj(func(json.RawMessage) (interface{}, error) {
		calls.Add(1)
		return yggcore.DebugGetPeersResponse{}, nil
	})
	obj.remoteSem = sem

	const callers = 32

	baseCtx, cancel := context.WithCancel(context.Background())
	observed := make(chan struct{}, callers)
	results := make(chan error, callers)
	start := make(chan struct{})
	for range callers {
		go func() {
			<-start
			ctx := &observedContextObj{Context: baseCtx, observed: observed}
			_, _, err := obj.callRemotePeers(ctx, key)
			results <- err
		}()
	}
	close(start)
	for range callers {
		<-observed
	}
	cancel()
	for range callers {
		if err := <-results; !errors.Is(err, context.Canceled) {
			t.Fatalf("caller error = %v, want context.Canceled", err)
		}
	}
	obj.remoteMu.RLock()
	flights := len(obj.remoteFlights)
	flight := obj.remoteFlights[toKeyArray(key)]
	obj.remoteMu.RUnlock()
	if flights != 1 || flight == nil {
		t.Fatalf("published flights = %d, want one flight for the requested key", flights)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("underlying calls while saturated = %d, want 0", got)
	}
	<-sem
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if _, _, err := waitRemoteFlight(waitCtx, flight); err != nil {
		t.Fatalf("accepted flight after capacity was released: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("underlying calls = %d, want 1", got)
	}
	_ = obj.Close()
}

func TestCallRemotePeersCapsAcceptedUniqueFlights(t *testing.T) {
	keys := genKeyN(t, 3)
	obj := newRemoteTestObj(func(json.RawMessage) (interface{}, error) {
		t.Fatal("over-cap call reached remote handler")
		return nil, nil
	})
	obj.remoteSem = make(chan struct{}, 2)
	for _, key := range keys[:2] {
		obj.remoteFlights[toKeyArray(key)] = &remoteFlightObj{done: make(chan struct{})}
	}

	if _, _, err := obj.callRemotePeers(context.Background(), keys[2]); !errors.Is(err, ErrProbeBusy) {
		t.Fatalf("new key error = %v, want ErrProbeBusy", err)
	}
	if got := len(obj.remoteFlights); got != 2 {
		t.Fatalf("accepted flights = %d, want 2", got)
	}

	existing := obj.remoteFlights[toKeyArray(keys[0])]
	existing.signal()
	if _, _, err := obj.callRemotePeers(context.Background(), keys[0]); err != nil {
		t.Fatalf("existing key must remain joinable at capacity: %v", err)
	}
}

func TestCallRemotePeers_canceledCallerDoesNotPoisonQueuedFlight(t *testing.T) {
	key := genKey(t)
	sem := make(chan struct{}, 1)
	sem <- struct{}{}
	var calls atomic.Int64
	obj := newRemoteTestObj(func(json.RawMessage) (interface{}, error) {
		calls.Add(1)
		return yggcore.DebugGetPeersResponse{}, nil
	})
	obj.remoteSem = sem

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, _, err := obj.callRemotePeers(ctx, key); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("call error = %v, want context.DeadlineExceeded", err)
	}
	obj.remoteMu.RLock()
	flight := obj.remoteFlights[toKeyArray(key)]
	obj.remoteMu.RUnlock()
	if flight == nil {
		t.Fatal("accepted flight was removed after its caller canceled")
	}

	<-sem
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if _, _, err := waitRemoteFlight(waitCtx, flight); err != nil {
		t.Fatalf("accepted flight after capacity was released: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("underlying calls after caller canceled = %d, want 1", got)
	}
	_ = obj.Close()
}

func TestCallRemotePeers_timeoutReturnsButKeepsUnderlyingCallOwned(t *testing.T) {
	key := genKey(t)
	started := make(chan struct{})
	release := make(chan struct{})
	obj := newRemoteTestObj(func(json.RawMessage) (interface{}, error) {
		close(started)
		<-release
		return yggcore.DebugGetPeersResponse{}, nil
	})
	obj.remoteTimeout = 10 * time.Millisecond
	obj.remoteSem = make(chan struct{}, 1)
	result := make(chan error, 1)
	go func() {
		_, _, err := obj.callRemotePeers(context.Background(), key)
		result <- err
	}()
	<-started
	if err := <-result; !errors.Is(err, ErrRemoteCallTimedOut) {
		t.Fatalf("call error = %v, want ErrRemoteCallTimedOut", err)
	}
	if got := len(obj.remoteSem); got != 1 {
		t.Fatalf("remote slots after timeout = %d, want 1", got)
	}
	closed := make(chan struct{})
	go func() {
		_ = obj.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Close abandoned the timed-out upstream call")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after upstream returned")
	}
}

// // // // // // // // // //
// AdminCaptureObj

func TestAdminCapture(t *testing.T) {
	capture := common.NewAdminCapture()
	fn := func(json.RawMessage) (interface{}, error) { return nil, nil }
	if err := capture.AddHandler("test_fn", "description", nil, fn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capture.Handlers["test_fn"] == nil {
		t.Fatal("handler not captured")
	}
	if capture.Handlers["missing"] != nil {
		t.Fatal("unexpected handler for missing key")
	}
}

// // // // // // // // // //

func BenchmarkParseRemotePeersResponse(b *testing.B) {
	keys := make([]string, 20)
	for i := range keys {
		pk, _, _ := ed25519.GenerateKey(rand.Reader)
		keys[i] = hex.EncodeToString(pk)
	}
	inner, _ := json.Marshal(struct {
		Keys []string `json:"keys"`
	}{Keys: keys})
	resp := yggcore.DebugGetPeersResponse{
		"node1": json.RawMessage(inner),
	}
	for b.Loop() {
		if _, _, err := parseRemotePeersResponse(resp, DefaultMaxPeersPerNode); err != nil {
			b.Fatalf("parseRemotePeersResponse: %v", err)
		}
	}
}
