package traceroute

import (
	"crypto/ed25519"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// buildTree — собирает дерево из плоского списка TreeEntryInfo.
// Каждый TreeEntryInfo содержит Key и Parent. Корнем считается узел,
// у которого Parent == Key (сам себе родитель), либо узел без родителя в наборе.
// Возвращает корень дерева с заполненными Children и Depth.
func buildTree(entries []yggcore.TreeEntryInfo) *NodeObj {
	if len(entries) == 0 {
		return nil
	}

	// индексируем все записи по ключу для быстрого поиска родителей
	index := make(map[[ed25519.PublicKeySize]byte]*NodeObj, len(entries))
	for _, e := range entries {
		k := toKeyArray(e.Key)
		index[k] = &NodeObj{
			Key:      e.Key,
			Parent:   e.Parent,
			Sequence: e.Sequence,
		}
	}

	// привязываем каждый узел к родителю; узел с Parent == Key — корень
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
			// сирота — родитель отсутствует в наборе; берём как кандидат в корень
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

// setDepth — рекурсивно проставляет глубину начиная с заданного значения
func setDepth(n *NodeObj, d int) {
	n.Depth = d
	for _, ch := range n.Children {
		setDepth(ch, d+1)
	}
}

// trimDepth — возвращает глубокую копию дерева, обрезанную на maxDepth.
// Узлы на глубине maxDepth не имеют потомков. maxDepth == 0 означает без ограничений.
func trimDepth(n *NodeObj, maxDepth int) *NodeObj {
	if n == nil {
		return nil
	}
	cp := &NodeObj{
		Key:      n.Key,
		Parent:   n.Parent,
		Sequence: n.Sequence,
		Depth:    n.Depth,
	}
	if maxDepth > 0 && n.Depth >= maxDepth {
		return cp
	}
	cp.Children = make([]*NodeObj, 0, len(n.Children))
	for _, ch := range n.Children {
		cp.Children = append(cp.Children, trimDepth(ch, maxDepth))
	}
	return cp
}

// // // // // // // // // //

// resolveHops — преобразует PathEntryInfo (путь как массив портов) в срез HopObj.
// Для каждого порта пытается найти соответствующий публичный ключ через GetPeers().
// Если порт не удалось сопоставить — Key в HopObj останется nil.
func resolveHops(path yggcore.PathEntryInfo, peers []yggcore.PeerInfo, tree []yggcore.TreeEntryInfo) []HopObj {
	// строим маппинг port → key из списка активных пиров
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

// toKeyArray — конвертирует ed25519.PublicKey (срез) в массив фиксированной длины.
// Нужен для использования в качестве ключа map.
func toKeyArray(key ed25519.PublicKey) [ed25519.PublicKeySize]byte {
	var arr [ed25519.PublicKeySize]byte
	copy(arr[:], key)
	return arr
}
