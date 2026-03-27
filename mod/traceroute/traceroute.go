package traceroute

import (
	"crypto/ed25519"
	"fmt"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// Obj — модуль исследования топологии сети Yggdrasil.
// Работает напрямую с ядром yggdrasil (Core), без admin socket.
// Все методы синхронные — каждый вызов запрашивает актуальные данные из ядра.
type Obj struct {
	core   *yggcore.Core  // ядро yggdrasil, передаётся при инициализации
	logger yggcore.Logger // логгер, обязателен
}

// New — создаёт модуль traceroute.
// Принимает указатель на ядро yggdrasil и логгер, оба обязательны.
func New(core *yggcore.Core, logger yggcore.Logger) (*Obj, error) {
	if core == nil {
		return nil, fmt.Errorf("traceroute: core is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("traceroute: logger is required")
	}
	return &Obj{core: core, logger: logger}, nil
}

// // // // // // // // // //

// Tree — возвращает spanning tree сети, известное данной ноде.
// maxDepth ограничивает глубину дерева (0 = без ограничения).
// Дерево строится из GetTree() — плоского списка parent→child связей.
// Корень — узел, у которого Parent == Key.
func (o *Obj) Tree(maxDepth int) *NodeObj {
	root := buildTree(o.core.GetTree())
	if root == nil {
		return nil
	}
	if maxDepth > 0 {
		return trimDepth(root, maxDepth)
	}
	return root
}

// Path — возвращает цепочку узлов от корня spanning tree до целевого ключа.
// Результат: [root, ..., промежуточные узлы, ..., target].
// Использует дерево из GetTree(), не raw-порты из GetPaths().
// Если ключ не найден в дереве — ошибка.
func (o *Obj) Path(key ed25519.PublicKey) ([]*NodeObj, error) {
	root := buildTree(o.core.GetTree())
	if root == nil {
		return nil, fmt.Errorf("traceroute: tree is empty")
	}
	path := root.PathTo(key)
	if path == nil {
		return nil, fmt.Errorf("traceroute: key not found in tree")
	}
	return path, nil
}

// Hops — возвращает маршрут до целевого ключа на уровне портов.
// Данные берутся из GetPaths() (активные маршруты ядра).
// Каждый порт резолвится в публичный ключ через GetPeers().
// Если активного пути нет — ошибка; вызови Lookup() сначала.
func (o *Obj) Hops(key ed25519.PublicKey) ([]HopObj, error) {
	paths := o.core.GetPaths()
	target := toKeyArray(key)
	for _, p := range paths {
		if toKeyArray(p.Key) == target {
			return resolveHops(p, o.core.GetPeers(), o.core.GetTree()), nil
		}
	}
	return nil, fmt.Errorf("traceroute: no active path to key")
}

// Lookup — запускает поиск пути до указанного ключа.
// Результат (маршрут) станет доступен через Hops() после завершения lookup.
// Это асинхронная операция на уровне ядра — мгновенного ответа не будет.
func (o *Obj) Lookup(key ed25519.PublicKey) {
	o.core.PacketConn.PacketConn.SendLookup(key)
}

// Peers — текущий список пиров из ядра
func (o *Obj) Peers() []yggcore.PeerInfo {
	return o.core.GetPeers()
}

// Sessions — активные сессии из ядра
func (o *Obj) Sessions() []yggcore.SessionInfo {
	return o.core.GetSessions()
}
