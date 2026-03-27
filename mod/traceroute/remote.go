package traceroute

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// adminCapture implements AddHandler to intercept handlers from core.SetAdmin.
// No real admin socket needed — just stores functions in a map.
type adminCapture struct {
	handlers map[string]yggcore.AddHandlerFunc
}

func (a *adminCapture) AddHandler(name, desc string, args []string, fn yggcore.AddHandlerFunc) error {
	a.handlers[name] = fn
	return nil
}

// // // // // // // // // //

type remoteCallResultObj struct {
	peers []ed25519.PublicKey
	err   error
}

// //

// callRemotePeers queries a remote node's peers via debug_remoteGetPeers.
// Called from pool workers. Returns immediately on ctx cancellation.
// The underlying o.remotePeers call (~6s yggdrasil timeout) may outlive the return —
// this is a bounded goroutine leak; the buffered channel prevents it from blocking.
func (o *Obj) callRemotePeers(ctx context.Context, key ed25519.PublicKey) ([]ed25519.PublicKey, error) {
	if o.remotePeers == nil {
		return nil, ErrRemotePeersDisabled
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	k := toKeyArray(key)
	if cached, ok := o.cache.get(k); ok {
		if cached == nil {
			return nil, ErrNodeUnreachable
		}
		return cached, nil
	}

	req, _ := json.Marshal(map[string]string{"key": hex.EncodeToString(key)})
	ch := make(chan remoteCallResultObj, 1)

	go func() {
		raw, err := o.remotePeers(req)
		if err != nil {
			ch <- remoteCallResultObj{err: err}
			return
		}
		peers, err := parseRemotePeersResponse(raw)
		ch <- remoteCallResultObj{peers: peers, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			o.logger.Debugf("[traceroute] remoteGetPeers failed for %x: %v", key[:8], r.err)
			o.cache.set(k, nil)
			return nil, r.err
		}
		o.cache.set(k, r.peers)
		return r.peers, nil
	}
}

// //

// parseRemotePeersResponse parses the debug_remoteGetPeers response.
// Format: {"<ipv6>": {"keys": ["hex1", "hex2", ...]}}
// Uses JSON roundtrip because the raw type from yggdrasil is not guaranteed.
func parseRemotePeersResponse(raw interface{}) ([]ed25519.PublicKey, error) {
	js, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("traceroute: marshal remote peers: %w", err)
	}

	var outer map[string]struct {
		Keys []string `json:"keys"`
	}
	if err := json.Unmarshal(js, &outer); err != nil {
		return nil, fmt.Errorf("traceroute: unmarshal remote peers: %w", err)
	}

	var peers []ed25519.PublicKey
	for _, inner := range outer {
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
