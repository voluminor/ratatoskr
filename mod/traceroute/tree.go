package traceroute

import (
	"crypto/ed25519"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// buildTree builds a tree from a flat list of TreeEntryInfo.
// Root is the node where Parent == Key (self-parented).
// Returns nil and an error if no self-rooted node is found.
func buildTree(entries []yggcore.TreeEntryInfo, logger yggcore.Logger) (*NodeObj, error) {
	if len(entries) == 0 {
		return nil, ErrTreeEmpty
	}

	index := make(map[[ed25519.PublicKeySize]byte]*NodeObj, len(entries))
	for _, e := range entries {
		k := toKeyArray(e.Key)
		index[k] = &NodeObj{
			Key:      e.Key,
			Parent:   e.Parent,
			Sequence: e.Sequence,
		}
	}

	orphans := 0
	var root *NodeObj
	for k, node := range index {
		pk := toKeyArray(node.Parent)
		if pk == k {
			root = node
			continue
		}
		if parent, ok := index[pk]; ok {
			parent.Children = append(parent.Children, node)
		} else {
			orphans++
		}
	}

	if orphans > 0 && logger != nil {
		logger.Warnf("[traceroute] buildTree: %d orphan nodes (parent not in tree)", orphans)
	}

	if root == nil {
		return nil, ErrNoRoot
	}
	setDepth(root, 0, 1024)
	return root, nil
}

// //

// setDepth recursively assigns depth starting from d.
// maxDepth guards against cycles or pathologically deep trees.
func setDepth(n *NodeObj, d, maxDepth int) {
	n.Depth = d
	if d >= maxDepth {
		n.Children = nil
		return
	}
	for _, ch := range n.Children {
		setDepth(ch, d+1, maxDepth)
	}
}

// // // // // // // // // //

// resolveHops converts PathEntryInfo into a HopObj slice.
// Ports are resolved to keys via GetPeers(); unresolved ports leave Key nil.
func resolveHops(path yggcore.PathEntryInfo, peers []yggcore.PeerInfo) []HopObj {
	portToKey := make(map[uint64]ed25519.PublicKey, len(peers))
	for _, p := range peers {
		if p.Up && p.Port > 0 {
			portToKey[p.Port] = p.Key
		}
	}

	hops := make([]HopObj, 0, len(path.Path))
	for i, port := range path.Path {
		hop := HopObj{Port: port, Depth: i}
		if key, ok := portToKey[port]; ok {
			hop.Key = key
		}
		hops = append(hops, hop)
	}
	return hops
}

// // // // // // // // // //

// toKeyArray converts ed25519.PublicKey to a fixed-size array for use as map keys.
func toKeyArray(key ed25519.PublicKey) [ed25519.PublicKeySize]byte {
	var arr [ed25519.PublicKeySize]byte
	copy(arr[:], key)
	return arr
}
