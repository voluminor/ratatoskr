package traceroute

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"

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

// Trace — комбинированный метод: ищет ключ в обоих источниках данных.
// 1) Spanning tree (GetTree) — parent→child цепочка до ключа.
// 2) Pathfinder (GetPaths) — port-based маршрут после SendLookup.
//
// Стратегия минимальной нагрузки:
// - Если оба источника уже имеют данные — возвращает мгновенно, без lookup.
// - Если tree есть, а hops нет — один SendLookup + короткое ожидание (до 2 сек).
// - Если ничего нет — SendLookup + полный poll до таймаута ctx.
// Повторный lookup отправляется не чаще раза в секунду.
func (o *Obj) Trace(ctx context.Context, key ed25519.PublicKey) (*TraceResultObj, error) {
	result := o.collect(key)

	// оба источника уже есть — мгновенный возврат
	if result != nil && result.TreePath != nil && result.Hops != nil {
		return result, nil
	}

	// хотя бы один источник пуст — нужен lookup для дообогащения
	o.Lookup(key)

	if result != nil {
		// tree уже есть, ждём только hops — короткий таймаут
		enriched := o.pollHops(ctx, key, 2*time.Second)
		if enriched != nil {
			result.Hops = enriched
		}
		return result, nil
	}

	// ничего нет — полный poll обоих источников
	o.logger.Infof("[traceroute] lookup started for %x", key[:8])
	return o.pollFull(ctx, key)
}

// //

// pollHops — ждёт появления hops до deadline. Один повторный lookup через секунду.
// Возвращает nil если не дождались.
func (o *Obj) pollHops(ctx context.Context, key ed25519.PublicKey, maxWait time.Duration) []HopObj {
	deadline := time.Now().Add(maxWait)
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	retried := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case t := <-ticker.C:
			if t.After(deadline) {
				// последняя попытка
				hops, _ := o.Hops(key)
				return hops
			}
			if hops, err := o.Hops(key); err == nil {
				return hops
			}
			// один повторный lookup через ~1 сек
			if !retried && time.Since(deadline.Add(-maxWait)) > time.Second {
				o.Lookup(key)
				retried = true
			}
		}
	}
}

// pollFull — полный poll: ищем и tree, и hops. Повтор lookup раз в секунду.
func (o *Obj) pollFull(ctx context.Context, key ed25519.PublicKey) (*TraceResultObj, error) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	lastLookup := time.Now()

	for {
		select {
		case <-ctx.Done():
			if result := o.collect(key); result != nil {
				return result, nil
			}
			return nil, fmt.Errorf("traceroute: lookup timed out for key %x", key[:8])
		case <-ticker.C:
			if result := o.collect(key); result != nil {
				// нашли хотя бы один источник — дообогащаем второй коротко
				if result.Hops == nil {
					o.Lookup(key)
					enriched := o.pollHops(ctx, key, 2*time.Second)
					if enriched != nil {
						result.Hops = enriched
					}
				}
				return result, nil
			}
			if time.Since(lastLookup) >= time.Second {
				o.Lookup(key)
				lastLookup = time.Now()
			}
		}
	}
}

// //

// collect — пробует собрать данные из обоих источников.
// Возвращает nil если ключ не найден ни в tree, ни в paths.
func (o *Obj) collect(key ed25519.PublicKey) *TraceResultObj {
	var result TraceResultObj

	if path, err := o.Path(key); err == nil {
		result.TreePath = path
	}
	if hops, err := o.Hops(key); err == nil {
		result.Hops = hops
	}

	if result.TreePath != nil || result.Hops != nil {
		return &result
	}
	return nil
}

// //

// Peers — текущий список пиров из ядра
func (o *Obj) Peers() []yggcore.PeerInfo {
	return o.core.GetPeers()
}

// Sessions — активные сессии из ядра
func (o *Obj) Sessions() []yggcore.SessionInfo {
	return o.core.GetSessions()
}
