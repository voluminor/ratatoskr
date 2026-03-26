package peermgr

import (
	"regexp"
	"sort"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// peerResultObj — результат пробинга одного пира
type peerResultObj struct {
	URI     string
	Proto   string
	Up      bool
	Latency time.Duration
}

// buildResults сопоставляет кандидатов с GetPeers(); отсутствующие → Up == false
func buildResults(candidates []string, peers []yggcore.PeerInfo) []peerResultObj {
	peerMap := make(map[string]yggcore.PeerInfo, len(peers))
	for _, p := range peers {
		peerMap[p.URI] = p
	}
	results := make([]peerResultObj, 0, len(candidates))
	for _, uri := range candidates {
		r := peerResultObj{URI: uri, Proto: parseProto(uri)}
		if p, ok := peerMap[uri]; ok {
			r.Up = p.Up
			r.Latency = p.Latency
		}
		results = append(results, r)
	}
	return results
}

// selectBest — топ-N пиров по протоколу среди Up==true, сортировка по латентности
func selectBest(results []peerResultObj, maxPerProto int) []string {
	groups := make(map[string][]peerResultObj)
	for _, r := range results {
		if !r.Up {
			continue
		}
		groups[r.Proto] = append(groups[r.Proto], r)
	}

	var selected []string
	for _, group := range groups {
		sort.Slice(group, func(i, j int) bool {
			return group[i].Latency < group[j].Latency
		})
		n := maxPerProto
		if n > len(group) {
			n = len(group)
		}
		for _, r := range group[:n] {
			selected = append(selected, r.URI)
		}
	}
	return selected
}

// countUp — количество Up == true
func countUp(results []peerResultObj) int {
	n := 0
	for _, r := range results {
		if r.Up {
			n++
		}
	}
	return n
}

var protoRe = regexp.MustCompile(`^([a-z]+)://`)

// parseProto — схема транспорта из URI ("tls://..." → "tls")
func parseProto(uri string) string {
	m := protoRe.FindStringSubmatch(uri)
	if len(m) < 2 {
		return "unknown"
	}
	return m[1]
}
