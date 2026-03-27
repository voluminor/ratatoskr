package traceroute

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

// adminCaptureObj implements AddHandler to intercept handlers from core.SetAdmin.
// No real admin socket needed — just stores functions in a map.
type adminCaptureObj struct {
	handlers map[string]yggcore.AddHandlerFunc
}

func (a *adminCaptureObj) AddHandler(name, desc string, args []string, fn yggcore.AddHandlerFunc) error {
	a.handlers[name] = fn
	return nil
}

// // // // // // // // // //

type remoteCallResultObj struct {
	peers []ed25519.PublicKey
	rtt   time.Duration
	err   error
}

// //

// callRemotePeers queries a remote node's peers via debug_remoteGetPeers.
// Called from pool workers. Returns immediately on ctx cancellation.
// The underlying o.remotePeers call (~6s yggdrasil timeout) may outlive the return —
// this is a bounded goroutine leak; the buffered channel prevents it from blocking.
func (o *Obj) callRemotePeers(ctx context.Context, key ed25519.PublicKey) ([]ed25519.PublicKey, time.Duration, error) {
	if o.remotePeers == nil {
		return nil, 0, ErrRemotePeersDisabled
	}
	if len(key) != ed25519.PublicKeySize {
		return nil, 0, ErrInvalidKeyLength
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
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

	go func() {
		start := time.Now()
		raw, err := o.remotePeers(req)
		rtt := time.Since(start)
		if err != nil {
			ch <- remoteCallResultObj{rtt: rtt, err: err}
			return
		}
		peers, err := parseRemotePeersResponse(raw)
		ch <- remoteCallResultObj{peers: peers, rtt: rtt, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			o.logger.Debugf("[traceroute] remoteGetPeers failed for %x: %v", key[:8], r.err)
			o.cache.set(k, nil, r.rtt)
			return nil, r.rtt, r.err
		}
		o.cache.set(k, r.peers, r.rtt)
		return r.peers, r.rtt, nil
	}
}

// //

// parseRemotePeersResponse parses the debug_remoteGetPeers response.
// Yggdrasil returns DebugGetPeersResponse (named map[string]interface{}) where
// each value is a json.RawMessage with {"keys": ["hex1", "hex2", ...]}.
func parseRemotePeersResponse(raw interface{}) ([]ed25519.PublicKey, error) {
	outer, ok := raw.(yggcore.DebugGetPeersResponse)
	if !ok {
		return nil, fmt.Errorf("traceroute: unexpected response type %T", raw)
	}

	var peers []ed25519.PublicKey
	for _, v := range outer {
		msg, ok := v.(json.RawMessage)
		if !ok {
			continue
		}
		var inner struct {
			Keys []string `json:"keys"`
		}
		if err := json.Unmarshal(msg, &inner); err != nil {
			continue
		}
		for _, hexKey := range inner.Keys {
			kbs, err := hex.DecodeString(hexKey)
			if err != nil || len(kbs) != ed25519.PublicKeySize {
				continue
			}
			peers = append(peers, ed25519.PublicKey(kbs))
		}
	}
	return peers, nil
}
