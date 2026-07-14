package probe

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

type remoteFlightObj struct {
	done     chan struct{}
	doneOnce sync.Once
	peers    []ed25519.PublicKey
	rtt      time.Duration
	err      error
}

func (f *remoteFlightObj) signal() { f.doneOnce.Do(func() { close(f.done) }) }

// remotePeerMessageObj is the per-node debug_remoteGetPeers payload; only the
// key list is consumed, other fields are ignored.
type remotePeerMessageObj struct {
	Keys []string `json:"keys"`
}

type remoteResultObj struct {
	peers []ed25519.PublicKey
	rtt   time.Duration
	err   error
}

const maxRemotePeerMessageBytes = 1024 * 1024

// //

func acquireRemoteSlot(lifecycle context.Context, sem chan struct{}) error {
	if sem == nil {
		return nil
	}
	select {
	case sem <- struct{}{}:
		return nil
	case <-lifecycle.Done():
		return lifecycle.Err()
	}
}

func releaseRemoteSlot(sem chan struct{}) {
	if sem != nil {
		<-sem
	}
}

// callRemotePeers queries a remote node's peers via debug_remoteGetPeers.
// Returns immediately on ctx cancellation; the underlying call (~6s timeout)
// may outlive the return. Calls for the same key share one underlying handler.
func (o *Obj) callRemotePeers(ctx context.Context, key ed25519.PublicKey) ([]ed25519.PublicKey, time.Duration, error) {
	if o.isClosed() {
		return nil, 0, ErrClosed
	}
	if o.remotePeers == nil {
		return nil, 0, ErrRemotePeersDisabled
	}
	if len(key) != ed25519.PublicKeySize {
		return nil, 0, ErrInvalidKeyLength
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}

	keyArray := toKeyArray(key)
	o.remoteMu.Lock()
	if o.closed || o.tasks == nil || o.remoteFlights == nil || o.remoteSem == nil {
		o.remoteMu.Unlock()
		return nil, 0, ErrClosed
	}
	if flight := o.remoteFlights[keyArray]; flight != nil {
		o.remoteMu.Unlock()
		return waitRemoteFlight(ctx, flight)
	}
	if len(o.remoteFlights) >= cap(o.remoteSem) {
		o.remoteMu.Unlock()
		return nil, 0, ErrProbeBusy
	}
	flight := &remoteFlightObj{done: make(chan struct{})}
	o.remoteFlights[keyArray] = flight
	tasks := o.tasks
	o.remoteMu.Unlock()
	// Publish before waiting for global capacity. The accepted flight belongs to
	// module lifecycle; caller cancellation only detaches that caller.
	if !tasks.Go(func(lifecycle context.Context) {
		o.runRemoteFlight(lifecycle, keyArray, flight, o.remoteSem)
	}) {
		o.finishRemoteFlight(keyArray, flight, ErrClosed)
	}
	return waitRemoteFlight(ctx, flight)
}

func waitRemoteFlight(ctx context.Context, flight *remoteFlightObj) ([]ed25519.PublicKey, time.Duration, error) {
	select {
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	case <-flight.done:
		if flight.err != nil {
			return nil, flight.rtt, flight.err
		}
		return clonePeerKeys(flight.peers), flight.rtt, nil
	}
}

func (o *Obj) finishRemoteFlight(key [ed25519.PublicKeySize]byte, flight *remoteFlightObj, err error) {
	if err != nil {
		flight.err = err
	}
	o.remoteMu.Lock()
	if o.remoteFlights[key] == flight {
		delete(o.remoteFlights, key)
	}
	o.remoteMu.Unlock()
	flight.signal()
}

func (o *Obj) runRemoteFlight(lifecycle context.Context, key [ed25519.PublicKeySize]byte, flight *remoteFlightObj, sem chan struct{}) {
	defer func() {
		o.finishRemoteFlight(key, flight, nil)
	}()
	if err := acquireRemoteSlot(lifecycle, sem); err != nil {
		flight.err = ErrClosed
		return
	}
	defer releaseRemoteSlot(sem)
	result := make(chan remoteResultObj, 1)
	go func() { result <- o.queryRemotePeers(key) }()

	var timer *time.Timer
	var timeout <-chan time.Time
	if o.remoteTimeout >= 0 {
		duration := o.remoteTimeout
		if duration == 0 {
			duration = defaultRemoteTimeout
		}
		timer = time.NewTimer(duration)
		timeout = timer.C
		defer timer.Stop()
	}
	select {
	case response := <-result:
		flight.peers = response.peers
		flight.rtt = response.rtt
		flight.err = response.err
		return
	case <-timeout:
		flight.err = ErrRemoteCallTimedOut
		flight.signal()
		<-result
		return
	case <-lifecycle.Done():
		flight.err = ErrClosed
		flight.signal()
		<-result
		return
	}
}

func (o *Obj) queryRemotePeers(key [ed25519.PublicKeySize]byte) remoteResultObj {
	req, _ := json.Marshal(map[string]string{"key": hex.EncodeToString(key[:])})
	start := time.Now()
	raw, err := o.remotePeers(req)
	rtt := time.Since(start)
	if err != nil {
		o.logger.Debugf("[probe] remoteGetPeers failed for %x: %v", key[:8], err)
		return remoteResultObj{rtt: rtt, err: err}
	}
	peers, truncated, err := parseRemotePeersResponse(raw, DefaultMaxPeersPerNode)
	if truncated {
		o.logger.Warnf("[probe] node %x returned more than %d peers, truncated to cap", key[:8], DefaultMaxPeersPerNode)
	}
	return remoteResultObj{peers: peers, rtt: rtt, err: err}
}

// //

// parseRemotePeersResponse parses DebugGetPeersResponse into a key list.
// The payload is already fully materialised in RAM, so keys are unmarshalled in
// one pass rather than streamed. Over-cap peer sets are truncated to the first
// limit valid keys (truncated=true) so an over-sharing node stays reachable with
// a bounded peer set; messages larger than maxRemotePeerMessageBytes are rejected.
func parseRemotePeersResponse(raw interface{}, limit int) ([]ed25519.PublicKey, bool, error) {
	outer, ok := raw.(yggcore.DebugGetPeersResponse)
	if !ok {
		return nil, false, fmt.Errorf("probe: unexpected response type %T", raw)
	}

	capacityHint := limit
	if capacityHint > 16 {
		capacityHint = 16
	}
	peers := make([]ed25519.PublicKey, 0, capacityHint)
	truncated := false
	for _, v := range outer {
		msg, ok := v.(json.RawMessage)
		if !ok {
			continue
		}
		next, cut, err := appendRemotePeerKeys(peers, msg, limit)
		if err != nil {
			return nil, false, err
		}
		peers = next
		truncated = truncated || cut
	}
	return peers, truncated, nil
}

// appendRemotePeerKeys decodes one node's payload and appends its valid keys,
// stopping once the accumulator reaches limit. Invalid or wrong-length hex keys
// are skipped without consuming a slot. Returns truncated=true when keys were
// dropped because the cap was reached.
func appendRemotePeerKeys(peers []ed25519.PublicKey, msg json.RawMessage, limit int) ([]ed25519.PublicKey, bool, error) {
	if len(msg) > maxRemotePeerMessageBytes {
		return nil, false, fmt.Errorf("%w: %d bytes", ErrRemoteResponseTooLarge, len(msg))
	}
	var decoded remotePeerMessageObj
	if err := json.Unmarshal(msg, &decoded); err != nil {
		// Malformed payloads contribute no peers rather than failing the node.
		return peers, false, nil
	}
	for _, hexKey := range decoded.Keys {
		if len(peers) >= limit {
			return peers, true, nil
		}
		kbs, err := hex.DecodeString(hexKey)
		if err != nil || len(kbs) != ed25519.PublicKeySize {
			continue
		}
		peers = append(peers, ed25519.PublicKey(kbs))
	}
	return peers, false, nil
}
