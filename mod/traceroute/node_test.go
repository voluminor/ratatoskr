package traceroute

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

// // // // // // // // // //
// NodeObj.Find

func TestFind_root(t *testing.T) {
	root, keys := buildTestTree(t)
	if found := root.Find(keys[0]); found != root {
		t.Fatal("expected root")
	}
}

func TestFind_deep(t *testing.T) {
	root, keys := buildTestTree(t)
	found := root.Find(keys[3])
	if found == nil || !found.Key.Equal(keys[3]) {
		t.Fatal("expected grandchild1")
	}
}

func TestFind_notFound(t *testing.T) {
	root, _ := buildTestTree(t)
	if root.Find(genKey(t)) != nil {
		t.Fatal("expected nil for missing key")
	}
}

func TestFind_nil(t *testing.T) {
	var n *NodeObj
	if n.Find(genKey(t)) != nil {
		t.Fatal("expected nil on nil receiver")
	}
}

// // // // // // // // // //
// NodeObj.Flatten

func TestFlatten(t *testing.T) {
	root, _ := buildTestTree(t)
	flat := root.Flatten()
	if len(flat) != 5 {
		t.Fatalf("expected 5 nodes, got %d", len(flat))
	}
	if flat[0] != root {
		t.Fatal("first element must be root")
	}
}

func TestFlatten_single(t *testing.T) {
	n := &NodeObj{Key: genKey(t)}
	flat := n.Flatten()
	if len(flat) != 1 {
		t.Fatalf("expected 1, got %d", len(flat))
	}
}

func TestFlatten_nil(t *testing.T) {
	var n *NodeObj
	if flat := n.Flatten(); flat != nil {
		t.Fatalf("expected nil, got %v", flat)
	}
}

// // // // // // // // // //
// NodeObj.PathTo

func TestPathTo_leaf(t *testing.T) {
	root, keys := buildTestTree(t)
	path := root.PathTo(keys[4])
	if path == nil {
		t.Fatal("expected path to grandchild2")
	}
	// root -> c1 -> gc2
	if len(path) != 3 {
		t.Fatalf("expected 3 hops, got %d", len(path))
	}
	if !path[0].Key.Equal(keys[0]) {
		t.Error("first hop must be root")
	}
	if !path[2].Key.Equal(keys[4]) {
		t.Error("last hop must be target")
	}
}

func TestPathTo_root(t *testing.T) {
	root, keys := buildTestTree(t)
	path := root.PathTo(keys[0])
	if len(path) != 1 {
		t.Fatalf("expected 1 hop for root self-path, got %d", len(path))
	}
}

func TestPathTo_notFound(t *testing.T) {
	root, _ := buildTestTree(t)
	if root.PathTo(genKey(t)) != nil {
		t.Fatal("expected nil for missing key")
	}
}

func TestPathTo_nil(t *testing.T) {
	var n *NodeObj
	if n.PathTo(genKey(t)) != nil {
		t.Fatal("expected nil on nil receiver")
	}
}

// // // // // // // // // //

func BenchmarkFind_deep(b *testing.B) {
	// Build a linear chain: root -> n1 -> n2 -> ... -> n99
	keys := make([]ed25519.PublicKey, 100)
	for i := range keys {
		pk, _, _ := ed25519.GenerateKey(rand.Reader)
		keys[i] = pk
	}
	root := &NodeObj{Key: keys[0]}
	cur := root
	for i := 1; i < len(keys); i++ {
		child := &NodeObj{Key: keys[i]}
		cur.Children = []*NodeObj{child}
		cur = child
	}
	target := keys[len(keys)-1]
	for b.Loop() {
		root.Find(target)
	}
}

func BenchmarkFlatten(b *testing.B) {
	keys := make([]ed25519.PublicKey, 1000)
	for i := range keys {
		pk, _, _ := ed25519.GenerateKey(rand.Reader)
		keys[i] = pk
	}
	// Binary-ish tree
	nodes := make([]*NodeObj, len(keys))
	for i, k := range keys {
		nodes[i] = &NodeObj{Key: k}
	}
	for i := range nodes {
		left := 2*i + 1
		right := 2*i + 2
		if left < len(nodes) {
			nodes[i].Children = append(nodes[i].Children, nodes[left])
		}
		if right < len(nodes) {
			nodes[i].Children = append(nodes[i].Children, nodes[right])
		}
	}
	root := nodes[0]
	for b.Loop() {
		root.Flatten()
	}
}

func BenchmarkPathTo(b *testing.B) {
	keys := make([]ed25519.PublicKey, 100)
	for i := range keys {
		pk, _, _ := ed25519.GenerateKey(rand.Reader)
		keys[i] = pk
	}
	root := &NodeObj{Key: keys[0]}
	cur := root
	for i := 1; i < len(keys); i++ {
		child := &NodeObj{Key: keys[i]}
		cur.Children = []*NodeObj{child}
		cur = child
	}
	target := keys[len(keys)-1]
	for b.Loop() {
		root.PathTo(target)
	}
}
