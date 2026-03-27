package traceroute

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// Obj — модуль исследования топологии сети Yggdrasil.
// Работает напрямую с ядром, без admin socket.
// Tree() делает BFS по пирам через debug_remoteGetPeers.
// Path(), Hops(), Trace() работают с локальными данными ядра.
type Obj struct {
	core        *yggcore.Core
	logger      yggcore.Logger
	remotePeers yggcore.AddHandlerFunc // debug_remoteGetPeers, захваченный через SetAdmin
}

// //

// adminCapture реализует AddHandler для перехвата обработчиков из core.SetAdmin.
// Не требует реального admin socket — просто сохраняет функции в map.
type adminCapture struct {
	handlers map[string]yggcore.AddHandlerFunc
}

func (a *adminCapture) AddHandler(name, desc string, args []string, fn yggcore.AddHandlerFunc) error {
	a.handlers[name] = fn
	return nil
}

// // // // // // // // // //

// New — создаёт модуль traceroute.
// Захватывает debug_remoteGetPeers через core.SetAdmin без поднятия admin socket.
func New(core *yggcore.Core, logger yggcore.Logger) (*Obj, error) {
	if core == nil {
		return nil, fmt.Errorf("traceroute: core is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("traceroute: logger is required")
	}

	capture := &adminCapture{handlers: make(map[string]yggcore.AddHandlerFunc)}
	_ = core.SetAdmin(capture)

	return &Obj{
		core:        core,
		logger:      logger,
		remotePeers: capture.handlers["debug_remoteGetPeers"],
	}, nil
}

// // // // // // // // // //

// Tree — builds a network topology tree via BFS.
// Root is our node; depth 1 is direct active peers from GetPeers().
// maxDepth > 0 required. concurrency <= 0 defaults to 16.
// Nodes that do not respond to peer queries are marked Unreachable.
func (o *Obj) Tree(ctx context.Context, maxDepth uint16, concurrency int) (*TreeResultObj, error) {
	return o.treeBFS(ctx, maxDepth, concurrency, nil)
}

// TreeChan — same as Tree but sends a TreeProgressObj to ch after each depth level.
// Done=true on the last message. ch is not closed by TreeChan.
func (o *Obj) TreeChan(ctx context.Context, maxDepth uint16, concurrency int, ch chan<- TreeProgressObj) (*TreeResultObj, error) {
	return o.treeBFS(ctx, maxDepth, concurrency, ch)
}

// //

// treeBFS — shared BFS implementation used by Tree and TreeChan.
// progress is nil-safe: no messages are sent when nil.
func (o *Obj) treeBFS(ctx context.Context, maxDepth uint16, concurrency int, progress chan<- TreeProgressObj) (*TreeResultObj, error) {
	if maxDepth == 0 {
		return nil, fmt.Errorf("traceroute: maxDepth must be > 0")
	}
	if concurrency <= 0 {
		concurrency = 16
	}

	selfKey := o.core.PublicKey()
	root := &NodeObj{Key: selfKey, Depth: 0}
	total := 0

	visited := make(map[[ed25519.PublicKeySize]byte]bool)
	visited[toKeyArray(selfKey)] = true

	// Level 1: active direct peers only.
	var currentLevel []*NodeObj
	for _, p := range o.core.GetPeers() {
		if !p.Up {
			continue
		}
		k := toKeyArray(p.Key)
		if visited[k] {
			continue
		}
		visited[k] = true
		child := &NodeObj{Key: p.Key, Depth: 1}
		root.Children = append(root.Children, child)
		currentLevel = append(currentLevel, child)
	}
	total += len(currentLevel)
	if progress != nil && len(currentLevel) > 0 {
		progress <- TreeProgressObj{Depth: 1, Found: len(currentLevel), Total: total}
	}

	// BFS levels 2..maxDepth.
	for depth := uint16(1); depth < maxDepth && len(currentLevel) > 0; depth++ {
		if ctx.Err() != nil {
			break
		}
		currentLevel = o.scanLevel(ctx, currentLevel, visited, concurrency, int(depth)+1)
		total += len(currentLevel)
		if progress != nil && len(currentLevel) > 0 {
			progress <- TreeProgressObj{Depth: int(depth) + 1, Found: len(currentLevel), Total: total}
		}
	}

	if progress != nil {
		progress <- TreeProgressObj{Done: true, Total: total}
	}

	return &TreeResultObj{Root: root, Total: total}, nil
}

// //

// scanLevel — параллельно запрашивает пиров у всех узлов текущего уровня.
// Горутины ограничены семафором concurrency. visited обновляется последовательно
// после того как все горутины завершены — это безопасно без мьютекса.
func (o *Obj) scanLevel(ctx context.Context, nodes []*NodeObj, visited map[[ed25519.PublicKeySize]byte]bool, concurrency, nextDepth int) []*NodeObj {
	type result struct {
		node  *NodeObj
		peers []ed25519.PublicKey
		err   error
	}

	results := make([]result, len(nodes))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, node := range nodes {
		wg.Add(1)
		go func(i int, node *NodeObj) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			peers, err := o.callRemotePeers(ctx, node.Key)
			results[i] = result{node: node, peers: peers, err: err}
		}(i, node)
	}
	wg.Wait()

	// последовательная обработка — visited без гонок
	var nextLevel []*NodeObj
	for _, r := range results {
		if r.err != nil {
			r.node.Unreachable = true
			continue
		}
		for _, peerKey := range r.peers {
			k := toKeyArray(peerKey)
			if visited[k] {
				continue
			}
			visited[k] = true
			child := &NodeObj{Key: peerKey, Depth: nextDepth}
			r.node.Children = append(r.node.Children, child)
			nextLevel = append(nextLevel, child)
		}
	}
	return nextLevel
}

// //

// callRemotePeers — запрашивает список пиров у удалённой ноды через debug_remoteGetPeers.
// Внутри yggdrasil таймаут 6 сек; если ctx истекает раньше — возвращаем ctx.Err().
func (o *Obj) callRemotePeers(ctx context.Context, key ed25519.PublicKey) ([]ed25519.PublicKey, error) {
	if o.remotePeers == nil {
		return nil, fmt.Errorf("traceroute: debug_remoteGetPeers unavailable")
	}

	req, _ := json.Marshal(map[string]string{"key": hex.EncodeToString(key)})

	type callResult struct {
		peers []ed25519.PublicKey
		err   error
	}
	ch := make(chan callResult, 1)

	go func() {
		raw, err := o.remotePeers(req)
		if err != nil {
			ch <- callResult{err: err}
			return
		}
		peers, err := parseRemotePeersResponse(raw)
		ch <- callResult{peers: peers, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		return r.peers, r.err
	}
}

// //

// parseRemotePeersResponse — разбирает ответ debug_remoteGetPeers.
// Формат ответа: {"<ipv6>": {"keys": ["hex1", "hex2", ...]}}
func parseRemotePeersResponse(raw interface{}) ([]ed25519.PublicKey, error) {
	js, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}

	var outer map[string]struct {
		Keys []string `json:"keys"`
	}
	if err := json.Unmarshal(js, &outer); err != nil {
		return nil, err
	}

	var peers []ed25519.PublicKey
	for _, inner := range outer {
		for _, hexKey := range inner.Keys {
			kbs, err := hex.DecodeString(hexKey)
			if err != nil || len(kbs) != ed25519.PublicKeySize {
				continue
			}
			peers = append(peers, ed25519.PublicKey(kbs))
		}
	}
	return peers, nil
}

// // // // // // // // // //

// Path — цепочка узлов от корня spanning tree до целевого ключа.
// Результат: [root, ..., target]. Использует локальный GetTree().
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

// Hops — маршрут до ключа на уровне портов из GetPaths().
// Каждый порт резолвится в ключ через GetPeers(). Требует предварительного Lookup().
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

// Lookup — инициирует поиск пути до ключа. Результат появится в Hops() через некоторое время.
func (o *Obj) Lookup(key ed25519.PublicKey) {
	o.core.PacketConn.PacketConn.SendLookup(key)
}

// Trace — ищет ключ в spanning tree и pathfinder одновременно.
// Стратегия минимальной нагрузки:
// - оба источника есть → возврат сразу
// - только tree → SendLookup + ожидание hops до 2 сек
// - ничего → полный poll с повтором lookup раз в секунду
func (o *Obj) Trace(ctx context.Context, key ed25519.PublicKey) (*TraceResultObj, error) {
	result := o.collect(key)

	if result != nil && result.TreePath != nil && result.Hops != nil {
		return result, nil
	}

	o.Lookup(key)

	if result != nil {
		enriched := o.pollHops(ctx, key, 2*time.Second)
		if enriched != nil {
			result.Hops = enriched
		}
		return result, nil
	}

	o.logger.Infof("[traceroute] lookup started for %x", key[:8])
	return o.pollFull(ctx, key)
}

// //

// pollHops — ждёт появления hops до deadline. Один повторный lookup через ~1 сек.
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
				hops, _ := o.Hops(key)
				return hops
			}
			if hops, err := o.Hops(key); err == nil {
				return hops
			}
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

// // // // // // // // // //

// Peers — прямые пиры из ядра
func (o *Obj) Peers() []yggcore.PeerInfo {
	return o.core.GetPeers()
}

// Sessions — активные сессии из ядра
func (o *Obj) Sessions() []yggcore.SessionInfo {
	return o.core.GetSessions()
}
