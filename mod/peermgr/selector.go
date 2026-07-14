package peermgr

import (
	"cmp"
	"sort"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// peerResultObj — probing result for a single peer
type peerResultObj struct {
	URI     string
	Proto   string
	Up      bool
	Latency time.Duration
}

// buildResults matches candidates against GetPeers(); missing ones → Up == false
func buildResults(candidates []peerEntryObj, peers []yggcore.PeerInfo) []peerResultObj {
	peerMap := make(map[string]yggcore.PeerInfo, len(peers))
	for _, p := range peers {
		key := normalizePeerURI(p.URI)
		current, ok := peerMap[key]
		if !ok || preferPeerInfo(p, current) {
			peerMap[key] = p
		}
	}
	results := make([]peerResultObj, 0, len(candidates))
	for _, c := range candidates {
		r := peerResultObj{URI: c.URI, Proto: c.Scheme}
		matchURI := c.MatchURI
		if matchURI == "" {
			matchURI = normalizePeerURI(c.URI)
		}
		if p, ok := peerMap[matchURI]; ok {
			r.Up = p.Up
			r.Latency = p.Latency
		}
		results = append(results, r)
	}
	return results
}

func preferPeerInfo(candidate, current yggcore.PeerInfo) bool {
	if candidate.Up != current.Up {
		return candidate.Up
	}
	if candidate.Up {
		return compareLatency(candidate.Latency, current.Latency) < 0
	}
	return false
}

func compareLatency(a, b time.Duration) int {
	if (a > 0) != (b > 0) {
		if a > 0 {
			return -1
		}
		return 1
	}
	return cmp.Compare(a, b)
}

func comparePeerResults(a, b peerResultObj) int {
	if c := compareLatency(a.Latency, b.Latency); c != 0 {
		return c
	}
	return cmp.Compare(normalizePeerURI(a.URI), normalizePeerURI(b.URI))
}

// selectBest — top-N peers per protocol among Up==true, sorted by latency
func selectBest(results []peerResultObj, maxPerProto int) []peerResultObj {
	if maxPerProto <= 0 {
		return nil
	}

	groups := make(map[string][]peerResultObj)
	for _, r := range results {
		if !r.Up {
			continue
		}
		groups[r.Proto] = append(groups[r.Proto], r)
	}

	var selected []peerResultObj
	for _, group := range groups {
		sort.Slice(group, func(i, j int) bool {
			return comparePeerResults(group[i], group[j]) < 0
		})
		n := maxPerProto
		if n > len(group) {
			n = len(group)
		}
		selected = append(selected, group[:n]...)
	}
	return selected
}

// countUp — count of Up == true
func countUp(results []peerResultObj) int {
	n := 0
	for _, r := range results {
		if r.Up {
			n++
		}
	}
	return n
}
