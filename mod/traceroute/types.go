package traceroute

import (
	"crypto/ed25519"
)

// // // // // // // // // //

// NodeObj — узел в spanning tree сети.
// Содержит публичный ключ, ссылку на родителя, и дочерние узлы.
// Является основным типом для представления топологии — используется
// как в полном дереве (Tree), так и в цепочке пути (Path).
type NodeObj struct {
	Key      ed25519.PublicKey // публичный ключ узла
	Parent   ed25519.PublicKey // публичный ключ родителя (у root совпадает с Key)
	Sequence uint64            // sequence number из протокола (свежесть записи)
	Depth    int               // глубина от корня дерева (root = 0)
	Children []*NodeObj        // дочерние узлы в spanning tree
}

// Find — рекурсивный поиск узла по ключу в поддереве.
// Возвращает указатель на найденный узел или nil.
func (n *NodeObj) Find(key ed25519.PublicKey) *NodeObj {
	if n == nil {
		return nil
	}
	if n.Key.Equal(key) {
		return n
	}
	for _, ch := range n.Children {
		if found := ch.Find(key); found != nil {
			return found
		}
	}
	return nil
}

// Flatten — обход дерева в глубину, возвращает плоский список всех узлов.
// Порядок: сначала текущий узел, потом рекурсивно все потомки.
func (n *NodeObj) Flatten() []*NodeObj {
	if n == nil {
		return nil
	}
	out := []*NodeObj{n}
	for _, ch := range n.Children {
		out = append(out, ch.Flatten()...)
	}
	return out
}

// PathTo — цепочка узлов от текущего (корня) до целевого ключа.
// Возвращает срез [root, ..., target] или nil если ключ не найден.
// Используется для получения маршрута через spanning tree.
func (n *NodeObj) PathTo(key ed25519.PublicKey) []*NodeObj {
	if n == nil {
		return nil
	}
	if n.Key.Equal(key) {
		return []*NodeObj{n}
	}
	for _, ch := range n.Children {
		if tail := ch.PathTo(key); tail != nil {
			return append([]*NodeObj{n}, tail...)
		}
	}
	return nil
}

// //

// HopObj — один хоп в маршруте на уровне портов.
// Получается из PathEntryInfo.Path ([]uint64) с резолвом портов в ключи.
// Key может быть nil если порт не удалось сопоставить с известным пиром.
type HopObj struct {
	Key   ed25519.PublicKey // публичный ключ узла (nil если не удалось резолвить)
	Port  uint64            // номер порта в spanning tree
	Depth int               // порядковый номер хопа (0 = первый)
}
