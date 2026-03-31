package peermgr

import (
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
		peerMap[p.URI] = p
	}
	results := make([]peerResultObj, 0, len(candidates))
	for _, c := range candidates {
		r := peerResultObj{URI: c.URI, Proto: c.Scheme}
		if p, ok := peerMap[c.URI]; ok {
			r.Up = p.Up
			r.Latency = p.Latency
		}
		results = append(results, r)
	}
	return results
}

// selectBest — top-N peers per protocol among Up==true, sorted by latency
func selectBest(results []peerResultObj, maxPerProto int) []peerResultObj {
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
			return group[i].Latency < group[j].Latency
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

// countUpActive counts Up peers among the active URI set
func countUpActive(activeURIs []string, peers []yggcore.PeerInfo) int {
	peerMap := make(map[string]bool, len(peers))
	for _, p := range peers {
		if p.Up {
			peerMap[p.URI] = true
		}
	}
	n := 0
	for _, uri := range activeURIs {
		if peerMap[uri] {
			n++
		}
	}
	return n
}
