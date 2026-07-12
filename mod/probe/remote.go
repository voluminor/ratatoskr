package probe

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

type remoteCallResultObj struct {
	peers []ed25519.PublicKey
	rtt   time.Duration
	err   error
}

// remotePeerMessageObj is the per-node debug_remoteGetPeers payload; only the
// key list is consumed, other fields are ignored.
type remotePeerMessageObj struct {
	Keys []string `json:"keys"`
}

const maxRemotePeerMessageBytes = 1024 * 1024

// //

func acquireRemoteSlot(ctx context.Context, sem chan struct{}) error {
	if sem == nil {
		return nil
	}
	select {
	case sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func releaseRemoteSlot(sem chan struct{}) {
	if sem != nil {
		<-sem
	}
}

func (o *Obj) startRemoteCall() error {
	o.remoteMu.RLock()
	defer o.remoteMu.RUnlock()
	if o.closed {
		return ErrClosed
	}
	o.remoteWG.Add(1)
	return nil
}

func (o *Obj) finishRemoteCall() {
	o.remoteWG.Done()
}

func (o *Obj) remoteClosed() bool {
	o.remoteMu.RLock()
	closed := o.closed
	o.remoteMu.RUnlock()
	return closed
}

// //

// callRemotePeers queries a remote node's peers via debug_remoteGetPeers.
// Returns immediately on ctx cancellation; the underlying call (~6s timeout)
// may outlive the return; in-flight calls are capped and joined by Close.
func (o *Obj) callRemotePeers(ctx context.Context, key ed25519.PublicKey) ([]ed25519.PublicKey, time.Duration, error) {
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
	if o.remoteClosed() {
		return nil, 0, ErrClosed
	}

	req, _ := json.Marshal(map[string]string{"key": hex.EncodeToString(key)})
	ch := make(chan remoteCallResultObj, 1)
	if err := acquireRemoteSlot(ctx, o.remoteSem); err != nil {
		return nil, 0, err
	}
	if err := o.startRemoteCall(); err != nil {
		releaseRemoteSlot(o.remoteSem)
		return nil, 0, err
	}

	go func() {
		defer o.finishRemoteCall()
		defer releaseRemoteSlot(o.remoteSem)
		start := time.Now()
		raw, err := o.remotePeers(req)
		rtt := time.Since(start)
		if err != nil {
			ch <- remoteCallResultObj{rtt: rtt, err: err}
			return
		}
		peers, truncated, err := parseRemotePeersResponse(raw, DefaultMaxPeersPerNode)
		if truncated {
			o.logger.Warnf("[probe] node %x returned more than %d peers, truncated to cap", key[:8], DefaultMaxPeersPerNode)
		}
		ch <- remoteCallResultObj{peers: peers, rtt: rtt, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			o.logger.Debugf("[probe] remoteGetPeers failed for %x: %v", key[:8], r.err)
			return nil, r.rtt, r.err
		}
		return r.peers, r.rtt, nil
	}
}

// //

// parseRemotePeersResponse parses DebugGetPeersResponse into a key list.
// The payload is already fully materialised in RAM, so keys are unmarshalled in
// one pass rather than streamed. Over-cap peer sets are truncated to the first
// limit valid keys (truncated=true) so an over-sharing node stays reachable with
// a bounded peer set; messages larger than maxRemotePeerMessageBytes are rejected.
func parseRemotePeersResponse(raw interface{}, limit int) ([]ed25519.PublicKey, bool, error) {
	if limit <= 0 {
		return nil, false, fmt.Errorf("%w: MaxPeersPerNode must be > 0", ErrInvalidConfig)
	}
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
