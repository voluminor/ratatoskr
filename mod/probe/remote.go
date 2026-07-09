package probe

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/voluminor/ratatoskr/internal/common"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

type remoteCallResultObj struct {
	peers []ed25519.PublicKey
	rtt   time.Duration
	err   error
}

const (
	maxRemotePeerMessageBytes = 1024 * 1024
	maxRemotePeerJSONTokens   = 8192
)

// //

func acquireRemoteSlot(ctx context.Context, limit *common.DynamicLimitObj) error {
	for {
		if limit == nil {
			return nil
		}
		acquired, ready := limit.AcquireOrReady()
		if acquired {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ready:
		}
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

	k := toKeyArray(key)
	if cached, rtt, ok := o.cache.get(k); ok {
		if cached == nil {
			return nil, rtt, ErrNodeUnreachable
		}
		return cached, rtt, nil
	}

	req, _ := json.Marshal(map[string]string{"key": hex.EncodeToString(key)})
	ch := make(chan remoteCallResultObj, 1)
	if err := acquireRemoteSlot(ctx, o.remoteLimit); err != nil {
		return nil, 0, err
	}
	if err := o.startRemoteCall(); err != nil {
		if o.remoteLimit != nil {
			o.remoteLimit.Release()
		}
		return nil, 0, err
	}

	go func() {
		defer o.finishRemoteCall()
		if o.remoteLimit != nil {
			defer o.remoteLimit.Release()
		}
		start := time.Now()
		raw, err := o.remotePeers(req)
		rtt := time.Since(start)
		if err != nil {
			o.cache.set(k, nil, rtt)
			ch <- remoteCallResultObj{rtt: rtt, err: err}
			return
		}
		peers, err := parseRemotePeersResponse(raw, o.maxPeersPerNode)
		if err != nil {
			o.cache.set(k, nil, rtt)
		} else {
			o.cache.set(k, peers, rtt)
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
func parseRemotePeersResponse(raw interface{}, limit int) ([]ed25519.PublicKey, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("%w: MaxPeersPerNode must be > 0", ErrInvalidConfig)
	}
	outer, ok := raw.(yggcore.DebugGetPeersResponse)
	if !ok {
		return nil, fmt.Errorf("probe: unexpected response type %T", raw)
	}

	capacityHint := limit
	if capacityHint > 16 {
		capacityHint = 16
	}
	peers := make([]ed25519.PublicKey, 0, capacityHint)
	for _, v := range outer {
		msg, ok := v.(json.RawMessage)
		if !ok {
			continue
		}
		next, err := appendRemotePeerKeys(peers, msg, limit)
		if err != nil {
			return nil, err
		}
		peers = next
	}
	return peers, nil
}

func appendRemotePeerKeys(peers []ed25519.PublicKey, msg json.RawMessage, limit int) ([]ed25519.PublicKey, error) {
	if len(msg) > maxRemotePeerMessageBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrRemoteResponseTooLarge, len(msg))
	}
	dec := json.NewDecoder(bytes.NewReader(msg))
	tok, err := dec.Token()
	if err != nil {
		return peers, nil
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return peers, nil
	}
	tokenBudget := maxRemotePeerJSONTokens
	for dec.More() {
		nameTok, err := dec.Token()
		if err != nil {
			return peers, nil
		}
		name, ok := nameTok.(string)
		if !ok {
			return peers, nil
		}
		if name != "keys" {
			if err := skipJSONValue(dec, &tokenBudget); err != nil {
				return peers, nil
			}
			continue
		}
		return decodeRemotePeerKeys(dec, peers, limit, &tokenBudget)
	}
	return peers, nil
}

func skipJSONValue(dec *json.Decoder, budget *int) error {
	if *budget <= 0 {
		return ErrRemoteResponseTooLarge
	}
	*budget--
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		for dec.More() {
			if *budget <= 0 {
				return ErrRemoteResponseTooLarge
			}
			*budget--
			if _, err = dec.Token(); err != nil {
				return err
			}
			if err = skipJSONValue(dec, budget); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err = skipJSONValue(dec, budget); err != nil {
				return err
			}
		}
	default:
		return nil
	}
	if *budget <= 0 {
		return ErrRemoteResponseTooLarge
	}
	*budget--
	_, err = dec.Token()
	return err
}

func decodeRemotePeerKeys(dec *json.Decoder, peers []ed25519.PublicKey, limit int, budget *int) ([]ed25519.PublicKey, error) {
	tok, err := dec.Token()
	if err != nil {
		return peers, nil
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '[' {
		return peers, nil
	}
	seen := 0
	for dec.More() {
		if *budget <= 0 {
			return nil, ErrRemoteResponseTooLarge
		}
		*budget--
		if seen >= limit {
			return nil, fmt.Errorf("%w: limit %d", ErrPeersPerNodeLimitExceeded, limit)
		}
		seen++
		var hexKey string
		if err := dec.Decode(&hexKey); err != nil {
			return peers, nil
		}
		kbs, err := hex.DecodeString(hexKey)
		if err != nil || len(kbs) != ed25519.PublicKeySize {
			continue
		}
		if len(peers) >= limit {
			return nil, fmt.Errorf("%w: limit %d", ErrPeersPerNodeLimitExceeded, limit)
		}
		peers = append(peers, ed25519.PublicKey(kbs))
	}
	_, _ = dec.Token()
	return peers, nil
}
