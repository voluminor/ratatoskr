package ninfo

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/mod/sigils/inet"
	"github.com/voluminor/ratatoskr/mod/sigils/sigil_core"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

func newTestObj() *Obj {
	ctx, cancel := context.WithCancel(context.Background())
	obj := &Obj{
		ctx:               ctx,
		cancel:            cancel,
		maxAskTime:        defaultMaxAskTime,
		askRetryPause:     defaultAskRetryPause,
		lookupInterval:    defaultLookupInterval,
		maxLookupTime:     defaultMaxLookupTime,
		closeWaitTime:     defaultCloseWaitTime,
		maxConcurrentAsks: defaultMaxConcurrentAsks,
		askFlights:        make(map[[ed25519.PublicKeySize]byte]*askFlightObj),
		sigils:            make(map[string]sigils.Interface),
	}
	return obj
}

// // // // // // // // // //
// AddSigil / GetSigil / DelSigil

func TestAddSigil_valid(t *testing.T) {
	obj := newTestObj()
	errs := obj.AddSigil(newMockSigil("test-sigil", "key1"))
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if obj.GetSigil("test-sigil") == nil {
		t.Fatal("sigil not found after add")
	}
}

func TestAddSigil_duplicate(t *testing.T) {
	obj := newTestObj()
	obj.AddSigil(newMockSigil("test-sigil"))
	errs := obj.AddSigil(newMockSigil("test-sigil"))
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
}

func TestAddSigil_invalidName(t *testing.T) {
	obj := newTestObj()
	errs := obj.AddSigil(newMockSigil("AB"))
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if obj.GetSigil("AB") != nil {
		t.Fatal("invalid sigil should not be stored")
	}
}

func TestAddSigil_reservedBuiltinName(t *testing.T) {
	obj := newTestObj()
	errs := obj.AddSigil(newMockSigil(inet.Name(), inet.Name()))
	if len(errs) != 1 {
		t.Fatalf("expected 1 reserved-name error, got %d", len(errs))
	}
	if obj.GetSigil(inet.Name()) != nil {
		t.Fatal("reserved built-in sigil should not be stored")
	}
}

func TestAddSigil_multiple(t *testing.T) {
	obj := newTestObj()
	errs := obj.AddSigil(
		newMockSigil("aaa"),
		newMockSigil("bbb"),
		newMockSigil("ccc"),
	)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(obj.sigils) != 3 {
		t.Fatalf("expected 3 sigils, got %d", len(obj.sigils))
	}
}

func TestAddSigil_nil(t *testing.T) {
	obj := newTestObj()
	errs := obj.AddSigil(nil)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
}

// //

func TestGetSigil_notFound(t *testing.T) {
	obj := newTestObj()
	if obj.GetSigil("nonexistent") != nil {
		t.Fatal("expected nil for missing sigil")
	}
}

// //

func TestDelSigil_valid(t *testing.T) {
	obj := newTestObj()
	obj.AddSigil(newMockSigil("test-sigil"))
	if err := obj.DelSigil("test-sigil"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if obj.GetSigil("test-sigil") != nil {
		t.Fatal("sigil should be removed")
	}
}

func TestDelSigil_notFound(t *testing.T) {
	obj := newTestObj()
	if err := obj.DelSigil("missing"); err == nil {
		t.Fatal("expected error for missing sigil")
	}
}

// // // // // // // // // //
// ImportSigils

func TestImportSigils_append(t *testing.T) {
	obj := newTestObj()
	obj.AddSigil(newMockSigil("existing"))

	src, _ := sigil_core.New(nil, newMockSigil("new-one"))
	errs := obj.ImportSigils(src)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if obj.GetSigil("new-one") == nil {
		t.Fatal("imported sigil not found")
	}
	if obj.GetSigil("existing") == nil {
		t.Fatal("existing sigil should be preserved")
	}
}

func TestImportSigils_doesNotCloneAlreadyClonedSourceSigils(t *testing.T) {
	obj := newTestObj()
	var clones atomic.Int64
	srcSigil := &cloneCountingSigilObj{
		mockSigilObj: newMockSigil("counted"),
		clones:       &clones,
	}
	src, errs := sigil_core.New(nil, srcSigil)
	if len(errs) != 0 {
		t.Fatalf("sigil_core.New errors: %v", errs)
	}
	clones.Store(0)

	errs = obj.ImportSigils(src)
	if len(errs) != 0 {
		t.Fatalf("ImportSigils errors: %v", errs)
	}
	if got := clones.Load(); got != 1 {
		t.Fatalf("ImportSigils should rely on sigil_core.Sigils clone only, got %d Clone calls", got)
	}
}

func TestImportSigils_append_conflict(t *testing.T) {
	obj := newTestObj()
	obj.AddSigil(newMockSigil("shared"))

	src, _ := sigil_core.New(nil, newMockSigil("shared"))
	errs := obj.ImportSigils(src)
	if len(errs) != 1 {
		t.Fatalf("expected 1 conflict error, got %d", len(errs))
	}
}

func TestImportSigils_skipsReservedBuiltinNames(t *testing.T) {
	obj := newTestObj()

	src, _ := sigil_core.New(nil, newMockSigil(inet.Name(), inet.Name()))
	errs := obj.ImportSigils(src)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if obj.GetSigil(inet.Name()) != nil {
		t.Fatal("reserved built-in sigil should not be imported")
	}
}

func TestImportSigils_conflictKeepsExisting(t *testing.T) {
	obj := newTestObj()
	old := newMockSigil("shared", "old-key")
	obj.AddSigil(old)

	replacement := newMockSigil("shared", "new-key")
	src, _ := sigil_core.New(nil, replacement)
	errs := obj.ImportSigils(src)
	if len(errs) != 1 {
		t.Fatalf("expected 1 conflict error, got %d", len(errs))
	}
	got := obj.GetSigil("shared")
	if got == nil {
		t.Fatal("conflict should keep existing sigil")
	}
	// Registration clones, so compare by data, not identity: the kept sigil must
	// carry the original's key, not the rejected import's.
	if _, ok := got.Params()["old-key"]; !ok {
		t.Fatal("conflict should keep the existing (old-key) sigil")
	}
	if _, ok := got.Params()["new-key"]; ok {
		t.Fatal("import must not overwrite the existing sigil")
	}
}

func TestImportSigils_nilSource(t *testing.T) {
	obj := newTestObj()
	errs := obj.ImportSigils(nil)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
}

// // // // // // // // // //
// sigilSlice

func TestSigilSlice_empty(t *testing.T) {
	obj := newTestObj()
	if obj.sigilSlice() != nil {
		t.Fatal("expected nil for empty sigils")
	}
}

func TestSigilSlice_populated(t *testing.T) {
	obj := newTestObj()
	obj.AddSigil(newMockSigil("aaa"), newMockSigil("bbb"))
	sl := obj.sigilSlice()
	if len(sl) != 2 {
		t.Fatalf("expected 2, got %d", len(sl))
	}
}

func TestSigils_concurrentAccess(t *testing.T) {
	obj := newTestObj()
	src, errs := sigil_core.New(nil,
		newMockSigil("src-one"),
		newMockSigil("src-two"),
	)
	if len(errs) != 0 {
		t.Fatalf("sigil_core.New: %v", errs)
	}

	const iterations = 2000
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = obj.GetSigil("src-one")
			_ = obj.sigilSlice()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			name := fmt.Sprintf("user-%d", i)
			obj.AddSigil(newMockSigil(name))
			_ = obj.DelSigil(name)
			_ = obj.ImportSigils(src)
		}
	}()
	wg.Wait()
}

// // // // // // // // // //
// Ask lifecycle

func TestAsk_afterCloseReturnsErrClosed(t *testing.T) {
	obj := newTestObj()
	obj.nodeInfo = func(json.RawMessage) (interface{}, error) {
		t.Fatal("nodeInfo should not be called after Close")
		return nil, nil
	}
	if err := obj.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := obj.Ask(context.Background(), make(ed25519.PublicKey, ed25519.PublicKeySize))
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

func TestAsk_inFlightCloseReturnsErrClosed(t *testing.T) {
	obj := newTestObj()
	release := make(chan struct{})
	handlerDone := make(chan struct{})
	obj.nodeInfo = func(json.RawMessage) (interface{}, error) {
		defer close(handlerDone)
		<-release
		return yggcore.GetNodeInfoResponse{}, errors.New("late response")
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := obj.Ask(context.Background(), make(ed25519.PublicKey, ed25519.PublicKeySize))
		errCh <- err
	}()
	time.Sleep(10 * time.Millisecond)
	closeDone := make(chan error, 1)
	go func() {
		closeDone <- obj.Close()
	}()
	select {
	case err := <-errCh:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("expected ErrClosed, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Ask did not return after Close")
	}
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
		t.Fatal("Close returned before handler finished")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("handler goroutine did not finish after release")
	}
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not return after handler finished")
	}
}

func TestAsk_canceledCallerKeepsSlotUntilHandlerDone(t *testing.T) {
	obj := newTestObj()
	obj.maxConcurrentAsks = 1
	started := make(chan struct{})
	release := make(chan struct{})
	handlerDone := make(chan struct{})
	var calls atomic.Int32
	obj.nodeInfo = func(json.RawMessage) (interface{}, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		defer close(handlerDone)
		<-release
		return yggcore.GetNodeInfoResponse{}, errors.New("late response")
	}

	ctx, cancel := context.WithCancel(context.Background())
	firstErr := make(chan error, 1)
	go func() {
		_, err := obj.Ask(ctx, make(ed25519.PublicKey, ed25519.PublicKeySize))
		firstErr <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first handler did not start")
	}
	cancel()
	select {
	case err := <-firstErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("first Ask did not return after caller cancel")
	}

	secondCtx, secondCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer secondCancel()
	_, err := obj.Ask(secondCtx, make(ed25519.PublicKey, ed25519.PublicKeySize))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected second Ask to wait for occupied slot, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("second handler should not start while abandoned handler holds slot, calls=%d", got)
	}
	close(release)
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("handler did not finish after release")
	}
	if err = obj.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestAsk_concurrentSameKeyCollapsesToOneCall(t *testing.T) {
	obj := newTestObj()
	obj.askRetryPause = -1 // one attempt per caller, no retries
	boom := errors.New("boom")
	var calls atomic.Int32
	proceed := make(chan struct{})
	obj.nodeInfo = func(json.RawMessage) (interface{}, error) {
		calls.Add(1)
		<-proceed // hold the flight open so concurrent asks join it
		return yggcore.GetNodeInfoResponse{}, boom
	}

	const n = 8
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			_, err := obj.Ask(context.Background(), make(ed25519.PublicKey, ed25519.PublicKeySize))
			errs <- err
		}()
	}
	time.Sleep(50 * time.Millisecond) // let every caller join the single flight
	close(proceed)

	for i := 0; i < n; i++ {
		select {
		case err := <-errs:
			if !errors.Is(err, boom) {
				t.Fatalf("caller %d: expected shared boom error, got %v", i, err)
			}
		case <-time.After(time.Second):
			t.Fatal("not all callers returned")
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected exactly one shared handler call, got %d", got)
	}
	if err := obj.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestAsk_flightCapRejectsExcessDistinctKeys(t *testing.T) {
	obj := newTestObj()
	obj.maxConcurrentAsks = 1 // cap distinct in-flight keys at 1
	release := make(chan struct{})
	started := make(chan struct{})
	obj.nodeInfo = func(json.RawMessage) (interface{}, error) {
		close(started)
		<-release
		return yggcore.GetNodeInfoResponse{}, errors.New("late response")
	}

	keyA := make(ed25519.PublicKey, ed25519.PublicKeySize)
	keyA[0] = 1
	go func() { _, _ = obj.Ask(context.Background(), keyA) }()
	<-started // flight A registered and holding the only slot

	keyB := make(ed25519.PublicKey, ed25519.PublicKeySize)
	keyB[0] = 2
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err := obj.Ask(ctx, keyB)
	if !errors.Is(err, ErrAskLimit) {
		t.Fatalf("expected ErrAskLimit for excess distinct key, got %v", err)
	}

	close(release)
	if err := obj.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestClose_timesOutWithoutWaitingForBlockedHandler(t *testing.T) {
	obj := newTestObj()
	obj.closeWaitTime = 10 * time.Millisecond
	started := make(chan struct{})
	release := make(chan struct{})
	handlerDone := make(chan struct{})
	obj.nodeInfo = func(json.RawMessage) (interface{}, error) {
		close(started)
		defer close(handlerDone)
		<-release
		return yggcore.GetNodeInfoResponse{}, errors.New("late response")
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := obj.Ask(context.Background(), make(ed25519.PublicKey, ed25519.PublicKeySize))
		errCh <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}

	err := obj.Close()
	if !errors.Is(err, ErrCloseTimedOut) {
		t.Fatalf("expected ErrCloseTimedOut, got %v", err)
	}
	select {
	case <-handlerDone:
		t.Fatal("handler finished before release")
	default:
	}
	select {
	case err = <-errCh:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("expected Ask to return ErrClosed, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Ask did not return after Close")
	}
	close(release)
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("handler did not finish after release")
	}
}

func TestAsk_backgroundContextIsBounded(t *testing.T) {
	want := errors.New("remote unavailable")
	obj := newTestObj()
	obj.maxAskTime = 20 * time.Millisecond
	obj.askRetryPause = time.Millisecond
	obj.nodeInfo = func(json.RawMessage) (interface{}, error) {
		return yggcore.GetNodeInfoResponse{}, want
	}
	start := time.Now()
	_, err := obj.Ask(context.Background(), make(ed25519.PublicKey, ed25519.PublicKeySize))
	if !errors.Is(err, want) {
		t.Fatalf("expected last remote error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("Ask was not bounded by MaxAskTime, elapsed=%s", elapsed)
	}
}

func TestParseAskResponse_rejectsOversizedNodeInfo(t *testing.T) {
	obj := newTestObj()
	raw := json.RawMessage(`{"data":"` + string(make([]byte, maxNodeInfoBytes)) + `"}`)
	_, err := obj.parseAskResponse(raw, time.Millisecond)
	if !errors.Is(err, ErrNodeInfoTooLarge) {
		t.Fatalf("expected ErrNodeInfoTooLarge, got %v", err)
	}
}

func TestZeroValueObjIsClosed(t *testing.T) {
	obj := &Obj{}
	if !obj.isClosed(obj.ctx) {
		t.Fatal("zero-value object should be treated as closed")
	}
	if obj.ctx != nil {
		t.Fatal("closed check should not initialize context")
	}
	if obj.maxConcurrentAsks != 0 {
		t.Fatal("closed check should not initialize ask limit")
	}
}

func TestAsk_maxConcurrentAsksLimitsBlockedHandlers(t *testing.T) {
	obj := newTestObj()
	obj.maxConcurrentAsks = 1
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	obj.nodeInfo = func(json.RawMessage) (interface{}, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return yggcore.GetNodeInfoResponse{}, errors.New("late response")
	}

	firstErr := make(chan error, 1)
	go func() {
		_, err := obj.Ask(context.Background(), make(ed25519.PublicKey, ed25519.PublicKeySize))
		firstErr <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first handler did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := obj.Ask(ctx, make(ed25519.PublicKey, ed25519.PublicKeySize))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline while waiting for ask slot, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("second handler should not start while slot is occupied, calls=%d", got)
	}

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- obj.Close()
	}()
	close(release)
	select {
	case err := <-firstErr:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("expected first Ask to close, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("first Ask did not return after Close")
	}
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not return after handler finished")
	}
}

func TestAskAddr_afterCloseReturnsErrClosed(t *testing.T) {
	obj := newTestObj()
	if err := obj.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := obj.AskAddr(context.Background(), "200::1")
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}
