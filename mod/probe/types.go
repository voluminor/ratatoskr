package probe

import (
	"crypto/ed25519"
	"time"
)

// // // // // // // // // //

// NodeObj is one node in a discovered topology path or tree.
type NodeObj struct {
	// Key is the node's public key.
	Key ed25519.PublicKey
	// Parent is the parent node's public key.
	Parent ed25519.PublicKey
	// Sequence is populated for spanning-tree paths.
	Sequence uint64
	// Depth is the distance from the root.
	Depth int
	// RTT is approximate for remote nodes: measures debug_remoteGetPeers round-trip
	// including multi-hop traversal, not pure network latency.
	RTT time.Duration
	// Unreachable reports a failed remote peer query during Tree.
	Unreachable bool
	// Children contains the next discovered depth level.
	Children []*NodeObj
}

// Find returns the first subtree node with key.
func (n *NodeObj) Find(key ed25519.PublicKey) *NodeObj {
	if n == nil {
		return nil
	}
	if n.Key.Equal(key) {
		return n
	}
	for _, ch := range n.Children {
		if found := ch.Find(key); found != nil {
			return found
		}
	}
	return nil
}

// Flatten returns the subtree in depth-first order.
func (n *NodeObj) Flatten() []*NodeObj {
	if n == nil {
		return nil
	}
	var out []*NodeObj
	n.flattenInto(&out)
	return out
}

func (n *NodeObj) flattenInto(out *[]*NodeObj) {
	*out = append(*out, n)
	for _, ch := range n.Children {
		ch.flattenInto(out)
	}
}

// PathTo returns the path from this node to key.
func (n *NodeObj) PathTo(key ed25519.PublicKey) []*NodeObj {
	if n == nil {
		return nil
	}
	var path []*NodeObj
	if n.pathTo(key, &path) {
		return path
	}
	return nil
}

func (n *NodeObj) pathTo(key ed25519.PublicKey, out *[]*NodeObj) bool {
	idx := len(*out)
	*out = append(*out, n)
	if n.Key.Equal(key) {
		return true
	}
	for _, ch := range n.Children {
		if ch.pathTo(key, out) {
			return true
		}
	}
	*out = (*out)[:idx]
	return false
}

// //

// HopObj is a single hop in the port-level route.
type HopObj struct {
	// Key is nil when Port cannot be mapped to a peer.
	Key ed25519.PublicKey
	// Port is the Yggdrasil pathfinder port.
	Port uint64
	// Index is the zero-based position in the path.
	Index int
}

// //

// TraceResultObj is the result of Trace(). Both fields may be populated.
type TraceResultObj struct {
	// TreePath is the available spanning-tree route.
	TreePath []*NodeObj
	// Hops is the available pathfinder route.
	Hops []HopObj
}

// //

// TreeResultObj is the result of Tree() and TreeChan().
type TreeResultObj struct {
	// Root is the local node.
	Root *NodeObj
	// Total excludes Root.
	Total int
	// Truncated reports that Limit stopped discovery.
	Truncated bool
	// Limit is the maximum discovered-node count excluding Root.
	Limit int
}

// TreeProgressObj is emitted after each BFS depth level.
type TreeProgressObj struct {
	// Depth is the completed BFS depth.
	Depth int
	// Found is the number of nodes at Depth.
	Found int
	// Total is the cumulative count excluding the root.
	Total int
	// Done marks the final progress value.
	Done bool
	// Truncated reports that Limit stopped discovery.
	Truncated bool
	// Limit is the maximum discovered-node count excluding the root.
	Limit int
}
