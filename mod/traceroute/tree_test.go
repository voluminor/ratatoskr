package traceroute

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //
// buildTree

func TestBuildTree_normal(t *testing.T) {
	keys := genKeyN(t, 4)
	entries := []yggcore.TreeEntryInfo{
		{Key: keys[0], Parent: keys[0], Sequence: 1}, // root
		{Key: keys[1], Parent: keys[0], Sequence: 2},
		{Key: keys[2], Parent: keys[0], Sequence: 3},
		{Key: keys[3], Parent: keys[1], Sequence: 4},
	}
	root, err := buildTree(entries, noopLoggerObj{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !root.Key.Equal(keys[0]) {
		t.Fatal("root key mismatch")
	}
	if len(root.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(root.Children))
	}
	if root.Depth != 0 {
		t.Fatalf("root depth must be 0, got %d", root.Depth)
	}
	flat := root.Flatten()
	if len(flat) != 4 {
		t.Fatalf("expected 4 nodes total, got %d", len(flat))
	}
}

func TestBuildTree_empty(t *testing.T) {
	_, err := buildTree(nil, noopLoggerObj{})
	if !errors.Is(err, ErrTreeEmpty) {
		t.Fatalf("expected ErrTreeEmpty, got: %v", err)
	}
}

func TestBuildTree_noRoot(t *testing.T) {
	keys := genKeyN(t, 2)
	entries := []yggcore.TreeEntryInfo{
		{Key: keys[0], Parent: keys[1]},
		{Key: keys[1], Parent: keys[0]},
	}
	_, err := buildTree(entries, noopLoggerObj{})
	if !errors.Is(err, ErrNoRoot) {
		t.Fatalf("expected ErrNoRoot, got: %v", err)
	}
}

func TestBuildTree_singleRoot(t *testing.T) {
	k := genKey(t)
	entries := []yggcore.TreeEntryInfo{{Key: k, Parent: k}}
	root, err := buildTree(entries, noopLoggerObj{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !root.Key.Equal(k) {
		t.Fatal("key mismatch")
	}
	if len(root.Children) != 0 {
		t.Fatal("single root should have no children")
	}
}

func TestBuildTree_orphans(t *testing.T) {
	keys := genKeyN(t, 3)
	entries := []yggcore.TreeEntryInfo{
		{Key: keys[0], Parent: keys[0]},   // root
		{Key: keys[1], Parent: keys[0]},   // valid child
		{Key: keys[2], Parent: genKey(t)}, // orphan: parent not in tree
	}
	root, err := buildTree(entries, noopLoggerObj{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(root.Flatten()) != 2 {
		t.Fatalf("expected 2 reachable nodes (root + child), got %d", len(root.Flatten()))
	}
}

// // // // // // // // // //
// setDepth

func TestSetDepth_normal(t *testing.T) {
	root, _ := buildTestTree(t)
	setDepth(root, 0, 100)
	for _, n := range root.Flatten() {
		if n.Depth < 0 {
			t.Fatal("negative depth")
		}
	}
	if root.Children[0].Children[0].Depth != 2 {
		t.Fatalf("expected depth 2 for grandchild, got %d", root.Children[0].Children[0].Depth)
	}
}

func TestSetDepth_cutoff(t *testing.T) {
	root, _ := buildTestTree(t)
	setDepth(root, 0, 1)
	// maxDepth=1 means children at depth 1 get their children cut
	for _, c := range root.Children {
		if len(c.Children) != 0 {
			t.Fatal("children beyond maxDepth should be cut")
		}
	}
}

// // // // // // // // // //

func BenchmarkBuildTree(b *testing.B) {
	n := 500
	keys := make([]ed25519.PublicKey, n)
	for i := range keys {
		pk, _, _ := ed25519.GenerateKey(rand.Reader)
		keys[i] = pk
	}
	entries := make([]yggcore.TreeEntryInfo, n)
	entries[0] = yggcore.TreeEntryInfo{Key: keys[0], Parent: keys[0]}
	for i := 1; i < n; i++ {
		entries[i] = yggcore.TreeEntryInfo{Key: keys[i], Parent: keys[(i-1)/2]}
	}
	log := noopLoggerObj{}
	for b.Loop() {
		buildTree(entries, log)
	}
}
