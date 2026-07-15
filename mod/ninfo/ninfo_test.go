package ninfo

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voluminor/ratatoskr/internal/common"
	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/mod/sigils/inet"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

func newTestObj() *Obj {
	obj := &Obj{
		tasks:          common.NewTaskGroup(context.Background()),
		maxAskTime:     defaultMaxAskTime,
		askRetryPause:  defaultAskRetryPause,
		lookupInterval: defaultLookupInterval,
		maxLookupTime:  defaultMaxLookupTime,
	}
	return obj
}

func TestNewRejectsNegativeLookupInterval(t *testing.T) {
	obj, err := New(ConfigObj{LookupInterval: -1})
	if err == nil {
		if obj != nil {
			_ = obj.Close()
		}
		t.Fatal("expected lookup interval error")
	}
	if !errors.Is(err, ErrInvalidLookupInterval) {
		t.Fatalf("New error = %v, want ErrInvalidLookupInterval", err)
	}
}

func TestNewRequiresSource(t *testing.T) {
	if _, err := New(ConfigObj{}); !errors.Is(err, ErrSourceRequired) {
		t.Fatalf("New error = %v, want ErrSourceRequired", err)
	}
}

// // // // // // // // // //

func TestCloneConfiguredSigilsOwnsOneClone(t *testing.T) {
	var clones atomic.Int64
	parser := &cloneCountingSigilObj{mockSigilObj: newMockSigil("custom", "key"), clones: &clones}
	configured, err := cloneConfiguredSigils([]sigils.Interface{parser})
	if err != nil {
		t.Fatal(err)
	}
	if len(configured) != 1 {
		t.Fatalf("configured sigils = %d, want 1", len(configured))
	}
	if got := clones.Load(); got != 1 {
		t.Fatalf("construction clones = %d, want 1", got)
	}
	parser.params[0] = "changed"
	if got := configured[0].GetParams()[0]; got != "key" {
		t.Fatalf("stored parser changed with caller-owned parser: %q", got)
	}
}

func TestCloneConfiguredSigilsRejectsInvalidSet(t *testing.T) {
	tests := []struct {
		name    string
		parsers []sigils.Interface
	}{
		{name: "nil", parsers: []sigils.Interface{nil}},
		{name: "invalid name", parsers: []sigils.Interface{newMockSigil("AB")}},
		{name: "reserved", parsers: []sigils.Interface{newMockSigil(inet.Name())}},
		{name: "duplicate", parsers: []sigils.Interface{newMockSigil("same"), newMockSigil("same")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := cloneConfiguredSigils(test.parsers); !errors.Is(err, ErrInvalidSigil) {
				t.Fatalf("error = %v, want ErrInvalidSigil", err)
			}
		})
	}
}

// // // // // // // // // //

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
	if obj.tasks != nil {
		t.Fatal("closed check should not initialize context")
	}
	if err := obj.Close(); err != nil {
		t.Fatalf("zero-value Close: %v", err)
	}
	if obj.tasks != nil {
		t.Fatal("zero-value Close should not initialize lifecycle state")
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
