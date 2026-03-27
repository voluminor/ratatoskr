package traceroute

import (
	"crypto/ed25519"
)

// // // // // // // // // //

// NodeObj — узел в дереве топологии сети.
// Используется в Tree() (BFS по пирам), Path() и Trace() (spanning tree).
// Unreachable выставляется в true только в Tree() если нода не ответила на запрос пиров.
type NodeObj struct {
	Key         ed25519.PublicKey // публичный ключ узла
	Parent      ed25519.PublicKey // ключ родителя (только в spanning tree режиме)
	Sequence    uint64            // sequence number (только в spanning tree режиме)
	Port        uint64            // порт для достижения узла от родителя (устарел, оставлен для совместимости)
	Depth       int               // глубина от корня (root = 0)
	Unreachable bool              // нода не ответила на запрос пиров в Tree()
	Children    []*NodeObj
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
// HopObj — один хоп в маршруте на уровне портов.
// Получается из PathEntryInfo.Path ([]uint64) с резолвом портов в ключи.
// Key может быть nil если порт не удалось сопоставить с известным пиром.
type HopObj struct {
	Key   ed25519.PublicKey // публичный ключ узла (nil если не удалось резолвить)
	Port  uint64            // номер порта в spanning tree
	Depth int               // порядковый номер хопа (0 = первый)
}

// //

// TraceResultObj — result of Trace().
// Both fields may be populated simultaneously.
// TreePath: path through spanning tree [root, ..., target]; nil if not in tree.
// Hops: pathfinder route port→key; nil if no active path.
type TraceResultObj struct {
	TreePath []*NodeObj
	Hops     []HopObj
}

// //

// TreeResultObj — result of Tree() and TreeChan().
type TreeResultObj struct {
	Root  *NodeObj
	Total int // total nodes found, excluding the root itself
}

// TreeProgressObj — progress update emitted after each BFS depth level.
// Done=true marks the final message; TreeChan returns immediately after sending it.
// The channel is not closed by TreeChan.
type TreeProgressObj struct {
	Depth int
	Found int  // nodes discovered at this depth level
	Total int  // cumulative total across all levels so far
	Done  bool // true on the last message — scan complete
}
