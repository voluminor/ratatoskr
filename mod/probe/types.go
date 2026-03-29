package probe

import (
	"crypto/ed25519"
	"time"
)

// // // // // // // // // //

// NodeObj is a node in the network topology tree.
// Used by Tree() (BFS), Path() and Trace() (spanning tree).
type NodeObj struct {
	Key      ed25519.PublicKey
	Parent   ed25519.PublicKey
	Sequence uint64 // spanning tree mode only
	Depth    int
	// RTT is approximate for remote nodes: measures debug_remoteGetPeers round-trip
	// including multi-hop traversal, not pure network latency.
	RTT         time.Duration
	Unreachable bool // did not respond to peer query (Tree only)
	Children    []*NodeObj
}

// Find recursively searches for a node by key. Returns nil if not found.
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

// Flatten returns a depth-first flat list of all nodes in the subtree.
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

// PathTo returns [root, ..., target] or nil if the key is not found.
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
	Key   ed25519.PublicKey // nil if port could not be resolved
	Port  uint64
	Index int
}

// //

// TraceResultObj is the result of Trace(). Both fields may be populated.
type TraceResultObj struct {
	TreePath []*NodeObj
	Hops     []HopObj
}

// //

// TreeResultObj is the result of Tree() and TreeChan().
type TreeResultObj struct {
	Root  *NodeObj
	Total int // excluding root
}

// TreeProgressObj is emitted after each BFS depth level.
type TreeProgressObj struct {
	Depth int
	Found int  // nodes at this depth level
	Total int  // cumulative total
	Done  bool // last message — scan complete
}
