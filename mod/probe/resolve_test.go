package probe

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //
// resolveHops

func TestResolveHops_allResolved(t *testing.T) {
	keys := genKeyN(t, 3)
	path := yggcore.PathEntryInfo{
		Key:  genKey(t),
		Path: []uint64{10, 20, 30},
	}
	peers := []yggcore.PeerInfo{
		{Key: keys[0], Port: 10, Up: true},
		{Key: keys[1], Port: 20, Up: true},
		{Key: keys[2], Port: 30, Up: true},
	}
	hops := resolveHops(path, peers)
	if len(hops) != 3 {
		t.Fatalf("expected 3 hops, got %d", len(hops))
	}
	for i, h := range hops {
		if h.Key == nil {
			t.Errorf("hop %d: expected resolved key", i)
		}
		if h.Index != i {
			t.Errorf("hop %d: expected Index=%d, got %d", i, i, h.Index)
		}
	}
}

func TestResolveHops_unresolved(t *testing.T) {
	path := yggcore.PathEntryInfo{
		Key:  genKey(t),
		Path: []uint64{10, 99},
	}
	peers := []yggcore.PeerInfo{
		{Key: genKey(t), Port: 10, Up: true},
	}
	hops := resolveHops(path, peers)
	if len(hops) != 2 {
		t.Fatalf("expected 2 hops, got %d", len(hops))
	}
	if hops[0].Key == nil {
		t.Error("hop 0 should be resolved")
	}
	if hops[1].Key != nil {
		t.Error("hop 1 should be unresolved")
	}
}

func TestResolveHops_downPeerIgnored(t *testing.T) {
	path := yggcore.PathEntryInfo{Key: genKey(t), Path: []uint64{10}}
	peers := []yggcore.PeerInfo{{Key: genKey(t), Port: 10, Up: false}}
	hops := resolveHops(path, peers)
	if hops[0].Key != nil {
		t.Error("down peer should not resolve")
	}
}

func TestResolveHops_empty(t *testing.T) {
	path := yggcore.PathEntryInfo{Key: genKey(t)}
	hops := resolveHops(path, nil)
	if len(hops) != 0 {
		t.Fatalf("expected 0 hops, got %d", len(hops))
	}
}

// // // // // // // // // //
// toKeyArray

func TestToKeyArray(t *testing.T) {
	key := genKey(t)
	arr := toKeyArray(key)
	for i := range arr {
		if arr[i] != key[i] {
			t.Fatalf("mismatch at byte %d", i)
		}
	}
}

func TestToKeyArray_mapEquality(t *testing.T) {
	key := genKey(t)
	a := toKeyArray(key)
	b := toKeyArray(key)
	if a != b {
		t.Fatal("same key should produce equal arrays")
	}
}

// // // // // // // // // //

func BenchmarkToKeyArray(b *testing.B) {
	pk, _, _ := ed25519.GenerateKey(rand.Reader)
	for b.Loop() {
		toKeyArray(pk)
	}
}

func BenchmarkResolveHops(b *testing.B) {
	peers := make([]yggcore.PeerInfo, 50)
	for i := range peers {
		pk, _, _ := ed25519.GenerateKey(rand.Reader)
		peers[i] = yggcore.PeerInfo{Key: pk, Port: uint64(i + 1), Up: true}
	}
	ports := make([]uint64, 30)
	for i := range ports {
		ports[i] = uint64(i + 1)
	}
	pk, _, _ := ed25519.GenerateKey(rand.Reader)
	path := yggcore.PathEntryInfo{Key: pk, Path: ports}

	for b.Loop() {
		resolveHops(path, peers)
	}
}
