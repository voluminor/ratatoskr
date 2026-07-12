package probe

import (
	"crypto/ed25519"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// spanningTreePath returns [root, ..., target] by walking parent links from the
// target up to the self-parented root, without materialising the whole tree.
func spanningTreePath(entries []yggcore.TreeEntryInfo, key ed25519.PublicKey) ([]*NodeObj, error) {
	if len(entries) == 0 {
		return nil, ErrTreeEmpty
	}
	index := make(map[[ed25519.PublicKeySize]byte]yggcore.TreeEntryInfo, len(entries))
	for _, e := range entries {
		index[toKeyArray(e.Key)] = e
	}
	cur := toKeyArray(key)
	if _, ok := index[cur]; !ok {
		return nil, ErrKeyNotInTree
	}
	// reversed holds target..root; the loop is bounded by the node count so a
	// cycle in malformed remote tree data cannot spin forever.
	reversed := make([]*NodeObj, 0, len(index))
	for i := 0; i <= len(index); i++ {
		e, ok := index[cur]
		if !ok {
			return nil, ErrNoRoot
		}
		reversed = append(reversed, &NodeObj{Key: e.Key, Parent: e.Parent, Sequence: e.Sequence})
		pk := toKeyArray(e.Parent)
		if pk == cur {
			path := make([]*NodeObj, len(reversed))
			for j, n := range reversed {
				n.Depth = len(reversed) - 1 - j
				path[n.Depth] = n
			}
			return path, nil
		}
		cur = pk
	}
	return nil, ErrNoRoot
}

// // // // // // // // // //

// resolveHops converts PathEntryInfo to HopObj slice, resolving ports to keys via peers.
func resolveHops(path yggcore.PathEntryInfo, peers []yggcore.PeerInfo) []HopObj {
	portToKey := make(map[uint64]ed25519.PublicKey, len(peers))
	for _, p := range peers {
		if p.Up && p.Port > 0 {
			portToKey[p.Port] = p.Key
		}
	}

	hops := make([]HopObj, 0, len(path.Path))
	for i, port := range path.Path {
		hop := HopObj{Port: port, Index: i}
		if key, ok := portToKey[port]; ok {
			hop.Key = key
		}
		hops = append(hops, hop)
	}
	return hops
}

// // // // // // // // // //

// toKeyArray converts ed25519.PublicKey to a fixed-size array for map keys.
func toKeyArray(key ed25519.PublicKey) [ed25519.PublicKeySize]byte {
	var arr [ed25519.PublicKeySize]byte
	copy(arr[:], key)
	return arr
}
