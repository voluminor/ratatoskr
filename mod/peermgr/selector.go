package peermgr

import (
	"net/url"
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

// buildResults сопоставляет список кандидатов с текущим состоянием GetPeers().
// Пиры не вернувшиеся в GetPeers() считаются недоступными (Up == false).
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

// selectBest выбирает топ-maxPerProto пиров по каждому протоколу среди Up==true,
// отсортированных по латентности (меньше — лучше).
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

// parseProto извлекает схему транспорта из URI пира ("tls://host:port" → "tls")
func parseProto(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme == "" {
		return "unknown"
	}
	return u.Scheme
}
