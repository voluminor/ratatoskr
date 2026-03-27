package traceroute

import (
	"crypto/ed25519"
)

// // // // // // // // // //

// NodeObj is a node in the network topology tree.
// Used by Tree() (BFS over peers), Path() and Trace() (spanning tree).
// Unreachable is set to true only in Tree() if the node did not respond to a peer query.
type NodeObj struct {
	Key         ed25519.PublicKey // node public key
	Parent      ed25519.PublicKey // parent key (spanning tree mode only)
	Sequence    uint64            // sequence number (spanning tree mode only)
	Depth       int               // depth from root (root = 0)
	Unreachable bool              // node did not respond to peer query in Tree()
	Children    []*NodeObj
}

// Find recursively searches for a node by key in the subtree.
// Returns a pointer to the found node or nil.
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

// Flatten performs a depth-first traversal, returning a flat list of all nodes.
// Order: current node first, then all descendants recursively.
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

// PathTo returns the node chain from the current node (root) to the target key.
// Returns [root, ..., target] or nil if the key is not found.
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
// Key may be nil if the port could not be resolved to a known peer.
type HopObj struct {
	Key   ed25519.PublicKey // node public key (nil if unresolvable)
	Port  uint64            // port number in spanning tree
	Index int               // hop ordinal (0 = first)
}

// //

// TraceResultObj — result of Trace().
// Both fields may be populated simultaneously.
// TreePath: path through spanning tree [root, ..., target]; nil if not in tree.
// Hops: pathfinder route port→key; nil if no active path.
type TraceResultObj struct {
	TreePath []*NodeObj
	Hops     []HopObj
}

// //

// TreeResultObj — result of Tree() and TreeChan().
type TreeResultObj struct {
	Root  *NodeObj
	Total int // total nodes found, excluding the root itself
}

// TreeProgressObj — progress update emitted after each BFS depth level.
// Done=true marks the final message; TreeChan returns immediately after sending it.
// The channel is not closed by TreeChan.
type TreeProgressObj struct {
	Depth int
	Found int  // nodes discovered at this depth level
	Total int  // cumulative total across all levels so far
	Done  bool // true on the last message — scan complete
}
