package peermgr

import (
	"testing"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

func makePeerInfo(uri string, up bool, latency time.Duration) yggcore.PeerInfo {
	return yggcore.PeerInfo{URI: uri, Up: up, Latency: latency}
}

// //

func TestBuildResults_allPresent(t *testing.T) {
	candidates := []peerEntryObj{
		{URI: "tls://a:1", Scheme: "tls"},
		{URI: "tcp://b:2", Scheme: "tcp"},
	}
	peers := []yggcore.PeerInfo{
		makePeerInfo("tls://a:1", true, 10*time.Millisecond),
		makePeerInfo("tcp://b:2", false, 0),
	}
	results := buildResults(candidates, peers)
	if len(results) != 2 {
		t.Fatalf("expected 2, got %d", len(results))
	}
	if !results[0].Up || results[0].Latency != 10*time.Millisecond {
		t.Errorf("result[0]: %+v", results[0])
	}
	if results[1].Up {
		t.Errorf("result[1] should be down: %+v", results[1])
	}
}

func TestBuildResults_missing(t *testing.T) {
	candidates := []peerEntryObj{{URI: "tls://a:1", Scheme: "tls"}}
	results := buildResults(candidates, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].Up {
		t.Error("missing peer should be Up=false")
	}
}

func TestBuildResults_orderPreserved(t *testing.T) {
	candidates := []peerEntryObj{
		{URI: "tls://a:1", Scheme: "tls"},
		{URI: "tls://b:2", Scheme: "tls"},
		{URI: "tls://c:3", Scheme: "tls"},
	}
	peers := []yggcore.PeerInfo{
		makePeerInfo("tls://c:3", true, 1*time.Millisecond),
		makePeerInfo("tls://a:1", true, 5*time.Millisecond),
	}
	results := buildResults(candidates, peers)
	if results[0].URI != "tls://a:1" {
		t.Errorf("expected first=tls://a:1, got %s", results[0].URI)
	}
	if results[2].URI != "tls://c:3" {
		t.Errorf("expected third=tls://c:3, got %s", results[2].URI)
	}
}

func TestBuildResults_schemeFromCandidate(t *testing.T) {
	candidates := []peerEntryObj{{URI: "quic://h:9000", Scheme: "quic"}}
	results := buildResults(candidates, nil)
	if results[0].Proto != "quic" {
		t.Errorf("expected proto=quic, got %s", results[0].Proto)
	}
}

func TestBuildResults_empty(t *testing.T) {
	results := buildResults(nil, nil)
	if len(results) != 0 {
		t.Errorf("expected empty, got %v", results)
	}
}

// //

func TestSelectBest_topN(t *testing.T) {
	results := []peerResultObj{
		{URI: "tls://a:1", Proto: "tls", Up: true, Latency: 50 * time.Millisecond},
		{URI: "tls://b:2", Proto: "tls", Up: true, Latency: 10 * time.Millisecond},
		{URI: "tls://c:3", Proto: "tls", Up: true, Latency: 30 * time.Millisecond},
		{URI: "tcp://d:4", Proto: "tcp", Up: true, Latency: 20 * time.Millisecond},
	}
	selected := selectBest(results, 2)
	tlsCount := 0
	for _, r := range selected {
		if r.Proto == "tls" {
			tlsCount++
		}
	}
	if tlsCount != 2 {
		t.Errorf("expected 2 tls, got %d", tlsCount)
	}
	if len(selected) != 3 {
		t.Errorf("expected 3 total (2 tls + 1 tcp), got %d", len(selected))
	}
}

func TestSelectBest_sortsByLatency(t *testing.T) {
	results := []peerResultObj{
		{URI: "tls://slow:1", Proto: "tls", Up: true, Latency: 100 * time.Millisecond},
		{URI: "tls://fast:2", Proto: "tls", Up: true, Latency: 5 * time.Millisecond},
	}
	selected := selectBest(results, 1)
	if len(selected) != 1 || selected[0].URI != "tls://fast:2" {
		t.Errorf("expected fastest, got %v", selected)
	}
}

func TestSelectBest_onlyUpPeers(t *testing.T) {
	results := []peerResultObj{
		{URI: "tls://a:1", Proto: "tls", Up: false},
		{URI: "tls://b:2", Proto: "tls", Up: true, Latency: 1 * time.Millisecond},
	}
	selected := selectBest(results, 2)
	if len(selected) != 1 || !selected[0].Up {
		t.Errorf("expected only up peers: %v", selected)
	}
}

func TestSelectBest_empty(t *testing.T) {
	if selected := selectBest(nil, 1); len(selected) != 0 {
		t.Errorf("expected empty, got %v", selected)
	}
}

func TestSelectBest_allDown(t *testing.T) {
	results := []peerResultObj{{URI: "tls://a:1", Proto: "tls", Up: false}}
	if selected := selectBest(results, 1); len(selected) != 0 {
		t.Errorf("expected empty, got %v", selected)
	}
}

func TestSelectBest_maxLargerThanGroup(t *testing.T) {
	results := []peerResultObj{
		{URI: "tls://a:1", Proto: "tls", Up: true, Latency: 10 * time.Millisecond},
	}
	selected := selectBest(results, 100)
	if len(selected) != 1 {
		t.Errorf("expected 1, got %d", len(selected))
	}
}

func TestSelectBest_multipleProtos(t *testing.T) {
	protos := []string{"tls", "tcp", "quic", "ws", "wss"}
	var results []peerResultObj
	for i, p := range protos {
		results = append(results, peerResultObj{
			URI:     p + "://host:1",
			Proto:   p,
			Up:      true,
			Latency: time.Duration(i+1) * time.Millisecond,
		})
	}
	selected := selectBest(results, 1)
	if len(selected) != len(protos) {
		t.Errorf("expected %d (one per proto), got %d", len(protos), len(selected))
	}
}

// //

func TestCountUp_mixed(t *testing.T) {
	results := []peerResultObj{{Up: true}, {Up: false}, {Up: true}}
	if n := countUp(results); n != 2 {
		t.Errorf("expected 2, got %d", n)
	}
}

func TestCountUp_allDown(t *testing.T) {
	results := []peerResultObj{{Up: false}, {Up: false}}
	if n := countUp(results); n != 0 {
		t.Errorf("expected 0, got %d", n)
	}
}

func TestCountUp_allUp(t *testing.T) {
	results := []peerResultObj{{Up: true}, {Up: true}, {Up: true}}
	if n := countUp(results); n != 3 {
		t.Errorf("expected 3, got %d", n)
	}
}

func TestCountUp_empty(t *testing.T) {
	if n := countUp(nil); n != 0 {
		t.Errorf("expected 0, got %d", n)
	}
}

// //

func BenchmarkBuildResults(b *testing.B) {
	candidates := make([]peerEntryObj, 50)
	peers := make([]yggcore.PeerInfo, 50)
	for i := range candidates {
		uri := "tls://host" + string(rune('a'+i%26)) + ":1234"
		candidates[i] = peerEntryObj{URI: uri, Scheme: "tls"}
		peers[i] = makePeerInfo(uri, i%3 != 0, time.Duration(i+1)*time.Millisecond)
	}
	for b.Loop() {
		buildResults(candidates, peers)
	}
}

func BenchmarkSelectBest(b *testing.B) {
	protos := []string{"tls", "tcp", "quic", "ws", "wss"}
	results := make([]peerResultObj, 100)
	for i := range results {
		results[i] = peerResultObj{
			URI:     "tls://h" + string(rune('a'+i%26)) + ":1",
			Proto:   protos[i%len(protos)],
			Up:      i%4 != 0,
			Latency: time.Duration(i+1) * time.Millisecond,
		}
	}
	for b.Loop() {
		selectBest(results, 3)
	}
}

func BenchmarkCountUp(b *testing.B) {
	results := make([]peerResultObj, 1000)
	for i := range results {
		results[i] = peerResultObj{Up: i%3 != 0}
	}
	for b.Loop() {
		countUp(results)
	}
}
