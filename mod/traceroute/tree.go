package traceroute

import (
	"crypto/ed25519"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// buildTree — собирает дерево из плоского списка TreeEntryInfo.
// Корень — узел у которого Parent == Key (сам себе родитель).
// Используется в Path() и Hops() / Trace() для spanning tree навигации.
func buildTree(entries []yggcore.TreeEntryInfo) *NodeObj {
	if len(entries) == 0 {
		return nil
	}

	// индексируем все записи по ключу
	index := make(map[[ed25519.PublicKeySize]byte]*NodeObj, len(entries))
	for _, e := range entries {
		k := toKeyArray(e.Key)
		index[k] = &NodeObj{
			Key:      e.Key,
			Parent:   e.Parent,
			Sequence: e.Sequence,
		}
	}

	// привязываем узлы к родителям; узел с Parent == Key — корень
	var root *NodeObj
	for k, node := range index {
		pk := toKeyArray(node.Parent)
		if pk == k {
			root = node
			continue
		}
		if parent, ok := index[pk]; ok {
			parent.Children = append(parent.Children, node)
		} else {
			// сирота — родитель не в наборе; кандидат в корень
			if root == nil {
				root = node
			}
		}
	}
	if root != nil {
		setDepth(root, 0)
	}
	return root
}

// //

// setDepth — рекурсивно проставляет глубину начиная с d
func setDepth(n *NodeObj, d int) {
	n.Depth = d
	for _, ch := range n.Children {
		setDepth(ch, d+1)
	}
}

// // // // // // // // // //

// resolveHops — преобразует PathEntryInfo в срез HopObj.
// Порты резолвятся в ключи через GetPeers(); если порт не найден — Key остаётся nil.
func resolveHops(path yggcore.PathEntryInfo, peers []yggcore.PeerInfo, tree []yggcore.TreeEntryInfo) []HopObj {
	portToKey := make(map[uint64]ed25519.PublicKey, len(peers))
	for _, p := range peers {
		if p.Up && p.Port > 0 {
			portToKey[p.Port] = p.Key
		}
	}

	hops := make([]HopObj, 0, len(path.Path))
	for i, port := range path.Path {
		hop := HopObj{Port: port, Depth: i}
		if key, ok := portToKey[port]; ok {
			hop.Key = key
		}
		hops = append(hops, hop)
	}
	return hops
}

// // // // // // // // // //

// toKeyArray — конвертирует ed25519.PublicKey в массив фиксированной длины для map-ключей.
func toKeyArray(key ed25519.PublicKey) [ed25519.PublicKeySize]byte {
	var arr [ed25519.PublicKeySize]byte
	copy(arr[:], key)
	return arr
}
