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
		ctx:            ctx,
		cancel:         cancel,
		maxAskTime:     defaultMaxAskTime,
		askRetryPause:  defaultAskRetryPause,
		lookupInterval: defaultLookupInterval,
		maxLookupTime:  defaultMaxLookupTime,
		sigils:         make(map[string]sigils.Interface),
	}
	return obj
}

// // // // // // // // // //
// AddSigil / GetSigil / DelSigil

func TestAddSigil_valid(t *testing.T) {
	obj := newTestObj()
	if err := obj.AddSigil(newMockSigil("test-sigil", "key1")); err != nil {
		t.Fatalf("unexpected errors: %v", err)
	}
	if obj.GetSigil("test-sigil") == nil {
		t.Fatal("sigil not found after add")
	}
}

func TestAddSigil_duplicate(t *testing.T) {
	obj := newTestObj()
	_ = obj.AddSigil(newMockSigil("test-sigil"))
	if err := obj.AddSigil(newMockSigil("test-sigil")); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestAddSigil_invalidName(t *testing.T) {
	obj := newTestObj()
	if err := obj.AddSigil(newMockSigil("AB")); err == nil {
		t.Fatal("expected invalid-name error")
	}
	if obj.GetSigil("AB") != nil {
		t.Fatal("invalid sigil should not be stored")
	}
}

func TestAddSigil_reservedBuiltinName(t *testing.T) {
	obj := newTestObj()
	if err := obj.AddSigil(newMockSigil(inet.Name(), inet.Name())); err == nil {
		t.Fatal("expected reserved-name error")
	}
	if obj.GetSigil(inet.Name()) != nil {
		t.Fatal("reserved built-in sigil should not be stored")
	}
}

func TestAddSigil_multiple(t *testing.T) {
	obj := newTestObj()
	if err := obj.AddSigil(
		newMockSigil("aaa"),
		newMockSigil("bbb"),
		newMockSigil("ccc"),
	); err != nil {
		t.Fatalf("unexpected errors: %v", err)
	}
	if len(obj.sigils) != 3 {
		t.Fatalf("expected 3 sigils, got %d", len(obj.sigils))
	}
}

func TestAddSigil_nil(t *testing.T) {
	obj := newTestObj()
	if err := obj.AddSigil(nil); err == nil {
		t.Fatal("expected nil-sigil error")
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
	_ = obj.AddSigil(newMockSigil("test-sigil"))
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
	_ = obj.AddSigil(newMockSigil("existing"))

	src, _ := sigil_core.New(nil, newMockSigil("new-one"))
	if err := obj.ImportSigils(src); err != nil {
		t.Fatalf("unexpected errors: %v", err)
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

	if err := obj.ImportSigils(src); err != nil {
		t.Fatalf("ImportSigils errors: %v", err)
	}
	if got := clones.Load(); got != 1 {
		t.Fatalf("ImportSigils should rely on sigil_core.Sigils clone only, got %d Clone calls", got)
	}
}

func TestImportSigils_append_conflict(t *testing.T) {
	obj := newTestObj()
	_ = obj.AddSigil(newMockSigil("shared"))

	src, _ := sigil_core.New(nil, newMockSigil("shared"))
	if err := obj.ImportSigils(src); err == nil {
		t.Fatal("expected conflict error")
	}
}

func TestImportSigils_skipsReservedBuiltinNames(t *testing.T) {
	obj := newTestObj()

	src, _ := sigil_core.New(nil, newMockSigil(inet.Name(), inet.Name()))
	if err := obj.ImportSigils(src); err != nil {
		t.Fatalf("unexpected errors: %v", err)
	}
	if obj.GetSigil(inet.Name()) != nil {
		t.Fatal("reserved built-in sigil should not be imported")
	}
}

func TestImportSigils_conflictKeepsExisting(t *testing.T) {
	obj := newTestObj()
	old := newMockSigil("shared", "old-key")
	_ = obj.AddSigil(old)

	replacement := newMockSigil("shared", "new-key")
	src, _ := sigil_core.New(nil, replacement)
	if err := obj.ImportSigils(src); err == nil {
		t.Fatal("expected conflict error")
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
	if err := obj.ImportSigils(nil); err == nil {
		t.Fatal("expected nil-source error")
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
	_ = obj.AddSigil(newMockSigil("aaa"), newMockSigil("bbb"))
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
			_ = obj.AddSigil(newMockSigil(name))
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

func TestCloseSynchronizesWithAskAdmission(t *testing.T) {
	obj := newTestObj()
	obj.askMu.Lock()
	closed := make(chan struct{})
	go func() {
		_ = obj.Close()
		close(closed)
	}()

	select {
	case <-closed:
		obj.askMu.Unlock()
		t.Fatal("Close returned while Ask admission mutex was held")
	case <-time.After(20 * time.Millisecond):
	}
	obj.askMu.Unlock()

	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not return after Ask admission mutex was released")
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

func TestAsk_sameKeySingleFlightAndCallerCancellation(t *testing.T) {
	obj := newTestObj()
	obj.askRetryPause = -1
	obj.askFlights = make(map[[ed25519.PublicKeySize]byte]*askFlightObj)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int64
	obj.nodeInfo = func(json.RawMessage) (interface{}, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return yggcore.GetNodeInfoResponse{"node": json.RawMessage(`{"name":"test"}`)}, nil
	}
	key := make(ed25519.PublicKey, ed25519.PublicKeySize)
	ctx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, err := obj.Ask(ctx, key)
		firstDone <- err
	}()
	<-started
	secondDone := make(chan error, 1)
	go func() {
		_, err := obj.Ask(context.Background(), key)
		secondDone <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("first caller error = %v, want context.Canceled", err)
	}
	close(release)
	if err := <-secondDone; err != nil {
		t.Fatalf("second caller: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("underlying calls = %d, want 1", got)
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
	if !obj.isClosed() {
		t.Fatal("zero-value object should be treated as closed")
	}
	if obj.ctx != nil {
		t.Fatal("closed check should not initialize context")
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
