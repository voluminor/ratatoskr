package peermgr

import (
	"context"
	"sort"
	"sync"
	"time"
)

// // // // // // // // // //

func stopTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func resetTimer(timer *time.Timer, interval time.Duration) {
	stopTimer(timer)
	timer.Reset(interval)
}

type runtimeTimerObj struct {
	timer *time.Timer
}

func (t *runtimeTimerObj) stop() {
	stopTimer(t.timer)
	t.timer = nil
}

// configure (re)arms the timer to a fixed interval; interval <= 0 disables it.
func (t *runtimeTimerObj) configure(interval time.Duration) <-chan time.Time {
	if interval <= 0 {
		t.stop()
		return nil
	}
	if t.timer == nil {
		t.timer = time.NewTimer(interval)
		return t.timer.C
	}
	resetTimer(t.timer, interval)
	return t.timer.C
}

func (m *Obj) lockOptimize(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case m.optimizeCh <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Obj) unlockOptimize() {
	<-m.optimizeCh
}

// //

func (m *Obj) run(ctx context.Context) {
	_ = m.optimizeLocked(ctx)

	hasWatch := m.cfg.MinPeers > 0

	// Watch goroutine signals when MinPeers threshold is confirmed
	var watchCh chan struct{}
	var watchWg sync.WaitGroup
	if hasWatch {
		watchCh = make(chan struct{}, 1)
		watchWg.Add(1)
		go func() {
			defer watchWg.Done()
			m.watchPeers(ctx, watchCh)
		}()
	}
	defer watchWg.Wait()

	refreshTimer := runtimeTimerObj{}
	defer refreshTimer.stop()
	refreshCh := refreshTimer.configure(m.cfg.RefreshInterval)
	for {
		select {
		case <-refreshCh:
			_ = m.optimizeLocked(ctx)
			refreshCh = refreshTimer.configure(m.cfg.RefreshInterval)
		case <-watchCh:
			_ = m.optimizeLocked(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// //

// watchPeers polls GetPeers at the fixed watch interval and sends on triggerCh
// when the active Up count stays at or below MinPeers for the fixed number of
// consecutive confirmations.
func (m *Obj) watchPeers(ctx context.Context, triggerCh chan<- struct{}) {
	threshold := int(m.cfg.MinPeers)
	confirmCount := 0

	watchTimer := runtimeTimerObj{}
	defer watchTimer.stop()
	watchCh := watchTimer.configure(m.watchInterval)

	for {
		select {
		case <-watchCh:
			if m.optimizing.Load() > 0 {
				confirmCount = 0
				watchCh = watchTimer.configure(m.watchInterval)
				continue
			}
			m.mu.Lock()
			active := m.active
			m.mu.Unlock()

			up := countUpActive(active, m.node.GetPeers())

			if up <= threshold {
				confirmCount++
				m.cfg.Logger.Debugf("[peermgr] MinPeers watch: %d up <= %d threshold, confirmation %d/%d",
					up, threshold, confirmCount, m.watchNeed)

				if confirmCount >= m.watchNeed {
					m.cfg.Logger.Warnf("[peermgr] active peers (%d) at or below MinPeers (%d) for %d checks, triggering re-evaluation",
						up, threshold, m.watchNeed)
					confirmCount = 0
					select {
					case triggerCh <- struct{}{}:
					default:
					}
				}
			} else {
				confirmCount = 0
			}
			watchCh = watchTimer.configure(m.watchInterval)
		case <-ctx.Done():
			return
		}
	}
}

// //

// optimizeLocked serializes optimize calls and runs callbacks after releasing the gate.
func (m *Obj) optimizeLocked(ctx context.Context) error {
	if err := m.lockOptimize(ctx); err != nil {
		return err
	}
	m.optimizing.Add(1)
	defer m.optimizing.Add(-1)

	m.mu.Lock()
	prevActive := append([]string(nil), m.active...)
	m.mu.Unlock()

	noReachable, err := func() (bool, error) {
		defer m.unlockOptimize()
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if m.cfg.MaxPerProto == -1 {
			return false, m.optimizePassive(ctx)
		}
		return m.optimizeActive(ctx)
	}()

	// Fire OnActiveChange when the managed set actually changed. A fresh copy is
	// snapshotted after the inner work so the internal slice never escapes.
	if m.cfg.OnActiveChange != nil {
		m.mu.Lock()
		curActive := append([]string(nil), m.active...)
		m.mu.Unlock()
		if activeSetChanged(prevActive, curActive) {
			m.dispatchActiveChange(curActive)
		}
	}

	if err == nil && noReachable && m.cfg.OnNoReachablePeers != nil {
		m.dispatchNoReachableCallback()
	}
	return err
}

// activeSetChanged reports whether two active URI sets differ, order-insensitive.
func activeSetChanged(prev, cur []string) bool {
	if len(prev) != len(cur) {
		return true
	}
	a := append([]string(nil), prev...)
	b := append([]string(nil), cur...)
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return true
		}
	}
	return false
}

// dispatchNoReachableCallback runs OnNoReachablePeers fire-and-forget with a
// single-flight guard and panic recovery; Stop never waits for it.
func (m *Obj) dispatchNoReachableCallback() {
	if !m.callbackActive.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer m.callbackActive.Store(false)
		defer func() {
			if r := recover(); r != nil {
				m.cfg.Logger.Errorf("[peermgr] OnNoReachablePeers panic: %v", r)
			}
		}()
		m.cfg.OnNoReachablePeers()
	}()
}

// dispatchActiveChange coalesces active-set changes while a prior callback is
// still running. Slow user code receives the newest pending value instead of an
// unbounded, unordered goroutine stream.
func (m *Obj) dispatchActiveChange(active []string) {
	cb := m.cfg.OnActiveChange
	if cb == nil {
		return
	}
	m.activeChangeMu.Lock()
	m.activePending = append([]string(nil), active...)
	if m.activeChangeOn {
		m.activeChangeMu.Unlock()
		return
	}
	m.activeChangeOn = true
	m.activeChangeMu.Unlock()

	go func() {
		for {
			m.activeChangeMu.Lock()
			pending := append([]string(nil), m.activePending...)
			m.activePending = nil
			m.activeChangeMu.Unlock()

			func() {
				defer func() {
					if r := recover(); r != nil {
						m.cfg.Logger.Errorf("[peermgr] OnActiveChange panic: %v", r)
					}
				}()
				cb(pending)
			}()

			m.activeChangeMu.Lock()
			if m.activePending == nil {
				m.activeChangeOn = false
				m.activeChangeMu.Unlock()
				return
			}
			m.activeChangeMu.Unlock()
		}
	}()
}

// //

func (m *Obj) probeCandidates(now time.Time) []peerEntryObj {
	m.mu.Lock()
	active := append([]string(nil), m.active...)
	m.mu.Unlock()

	activeSet := make(map[string]struct{}, len(active))
	for _, uri := range active {
		activeSet[normalizePeerURI(uri)] = struct{}{}
	}
	out := make([]peerEntryObj, 0, len(active))
	seen := make(map[string]struct{}, len(m.peers))
	for _, peer := range m.peers {
		key := peer.MatchURI
		if key == "" {
			key = normalizePeerURI(peer.URI)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		_, active := activeSet[key]
		state := m.probeState[key]
		if active || state.nextTry.IsZero() || !now.Before(state.nextTry) {
			out = append(out, peer)
			seen[key] = struct{}{}
		}
	}
	return out
}

func (m *Obj) activeSet() map[string]struct{} {
	m.mu.Lock()
	active := append([]string(nil), m.active...)
	m.mu.Unlock()
	out := make(map[string]struct{}, len(active))
	for _, uri := range active {
		out[normalizePeerURI(uri)] = struct{}{}
	}
	return out
}

func peerEntryKey(peer peerEntryObj) string {
	if peer.MatchURI != "" {
		return peer.MatchURI
	}
	return normalizePeerURI(peer.URI)
}

func (m *Obj) updateProbeBackoff(results []peerResultObj, probeTimeout time.Duration) {
	now := time.Now()
	for _, r := range results {
		key := normalizePeerURI(r.URI)
		if r.Up {
			delete(m.probeState, key)
			continue
		}
		state := m.probeState[key]
		state.failures++
		delay := probeTimeout
		if delay <= 0 {
			delay = defaultProbeTimeout
		}
		for i := 0; i < state.failures; i++ {
			delay *= 2
			if delay >= maxProbeBackoff {
				delay = maxProbeBackoff
				break
			}
		}
		state.nextTry = now.Add(delay)
		m.probeState[key] = state
	}
}

func waitProbeBatch(ctx context.Context, probeTimeout time.Duration) error {
	timer := time.NewTimer(probeTimeout)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func effectiveBatchSize(size, total int) int {
	if total <= 0 {
		return 0
	}
	if size <= 1 {
		size = defaultBatchSize
	}
	if size > maxBatchSize {
		size = maxBatchSize
	}
	if size > total {
		return total
	}
	return size
}

// optimizeActive runs a sliding tournament: each batch gets a full probe window before selection.
func (m *Obj) optimizeActive(ctx context.Context) (bool, error) {
	probeTimeout := m.cfg.ProbeTimeout
	m.mu.Lock()
	oldActive := append([]string(nil), m.active...)
	m.mu.Unlock()
	peers := m.probeCandidates(time.Now())
	if len(peers) == 0 {
		m.cfg.Logger.Debugf("[peermgr] no candidates due for probing")
		return false, nil
	}
	activeSet := m.activeSet()
	batchSize := m.cfg.BatchSize
	batchSize = effectiveBatchSize(batchSize, len(peers))

	connected := make([]peerEntryObj, 0, len(peers))
	pending := make([]peerEntryObj, 0, batchSize)
	processed := make(map[string]struct{}, len(peers))
	totalBatches := (len(peers) + batchSize - 1) / batchSize
	// Roll back a cycle aborted mid-tournament. The only trigger now is Stop
	// cancelling the manager ctx, so this keeps mid-batch and not-yet-reprobed
	// peers in the active set for Stop's teardown instead of leaking them.
	committed := false
	defer func() {
		if committed {
			return
		}
		for _, p := range pending {
			if err := m.node.RemovePeer(p.URI); err != nil {
				m.cfg.Logger.Debugf("[peermgr] RemovePeer pending %s: %v", normalizePeerURI(p.URI), err)
			}
		}
		m.setActive(mergeRetainedActive(m.Active(), oldActive, processed))
	}()

	for i := 0; i < len(peers); i += batchSize {
		end := i + batchSize
		if end > len(peers) {
			end = len(peers)
		}
		batch := peers[i:end]
		batchIdx := i/batchSize + 1

		m.cfg.Logger.Debugf("[peermgr] batch %d/%d: adding %d candidates", batchIdx, totalBatches, len(batch))

		for _, p := range batch {
			if _, active := activeSet[peerEntryKey(p)]; active {
				// Already-connected candidate stays in the tournament.
				connected = append(connected, p)
				continue
			}
			if err := m.node.AddPeer(p.URI); err != nil {
				// A peer we could not add never enters the tournament, so a failed
				// dial never triggers RemovePeer on a URI we do not manage.
				m.cfg.Logger.Debugf("[peermgr] AddPeer %s: %v", normalizePeerURI(p.URI), err)
				continue
			}
			pending = append(pending, p)
			connected = append(connected, p)
		}

		m.cfg.Logger.Traceln("[peermgr] batch", batchIdx, "/", totalBatches, "waiting", probeTimeout, ",", len(connected), "connected")

		if err := waitProbeBatch(ctx, probeTimeout); err != nil {
			return false, err
		}

		var err error
		connected, err = m.probeAndSelect(connected, batchIdx, totalBatches, probeTimeout)
		if err != nil {
			return false, err
		}
		for _, p := range batch {
			processed[peerEntryKey(p)] = struct{}{}
		}
		pending = pending[:0]
	}

	noReachable := m.reportResult(connected)
	committed = true
	return noReachable, nil
}

// probeAndSelect — selects best peers, removes losers, updates m.active
func (m *Obj) probeAndSelect(connected []peerEntryObj, batchIdx, totalBatches int, probeTimeout time.Duration) ([]peerEntryObj, error) {
	results := buildResults(connected, m.node.GetPeers())
	m.updateProbeBackoff(results, probeTimeout)
	selected := selectBest(results, m.cfg.MaxPerProto)

	m.cfg.Logger.Debugf("[peermgr] batch %d/%d: %d up, %d selected, %d dropped",
		batchIdx, totalBatches, countUp(results), len(selected), len(connected)-len(selected))

	selectedSet := make(map[string]bool, len(selected))
	for _, r := range selected {
		selectedSet[r.URI] = true
	}

	kept := make([]peerEntryObj, 0, len(selected))
	uris := make([]string, 0, len(selected))
	for _, p := range connected {
		if selectedSet[p.URI] {
			kept = append(kept, p)
			uris = append(uris, p.URI)
		} else {
			m.cfg.Logger.Traceln("[peermgr] batch", batchIdx, "/", totalBatches, "removing loser", normalizePeerURI(p.URI))
			if err := m.node.RemovePeer(p.URI); err != nil {
				m.cfg.Logger.Debugf("[peermgr] RemovePeer %s: %v", normalizePeerURI(p.URI), err)
			}
		}
	}

	m.mu.Lock()
	m.active = uris
	m.mu.Unlock()

	return kept, nil
}

// reportResult logs the outcome and reports whether no peer was reachable.
func (m *Obj) reportResult(connected []peerEntryObj) bool {
	if len(connected) == 0 {
		m.cfg.Logger.Warnf("[peermgr] no reachable peers after probe")
		return true
	}
	uris := make([]string, len(connected))
	for i, p := range connected {
		uris[i] = normalizePeerURI(p.URI)
	}
	m.cfg.Logger.Infof("[peermgr] %d active peers", len(uris))
	m.cfg.Logger.Debugf("[peermgr] active peers: %v", uris)
	return false
}

func (m *Obj) setActive(active []string) {
	m.mu.Lock()
	m.active = append([]string(nil), active...)
	m.mu.Unlock()
}

func mergeRetainedActive(current []string, oldActive []string, processed map[string]struct{}) []string {
	out := make([]string, 0, len(current)+len(oldActive))
	seen := make(map[string]struct{}, len(current)+len(oldActive))
	for _, uri := range current {
		key := normalizePeerURI(uri)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, uri)
	}
	for _, uri := range oldActive {
		key := normalizePeerURI(uri)
		if _, done := processed[key]; done {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, uri)
	}
	return out
}

// //

// optimizePassive ensures every configured peer was submitted at least once.
func (m *Obj) optimizePassive(ctx context.Context) error {
	m.mu.Lock()
	oldActive := append([]string(nil), m.active...)
	m.mu.Unlock()
	activeSet := make(map[string]struct{}, len(oldActive))
	for _, uri := range oldActive {
		activeSet[normalizePeerURI(uri)] = struct{}{}
	}
	added := make([]string, 0, len(m.peers))
	processed := make(map[string]struct{}, len(m.peers))
	committed := false
	defer func() {
		if committed {
			return
		}
		m.setActive(mergeRetainedActive(added, oldActive, processed))
	}()
	for _, p := range m.peers {
		if err := ctx.Err(); err != nil {
			return err
		}
		key := peerEntryKey(p)
		processed[key] = struct{}{}
		if _, active := activeSet[key]; active {
			added = append(added, p.URI)
			continue
		}
		if err := m.node.AddPeer(p.URI); err != nil {
			m.cfg.Logger.Debugf("[peermgr] AddPeer %s: %v", normalizePeerURI(p.URI), err)
		} else {
			added = append(added, p.URI)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	m.setActive(added)

	m.cfg.Logger.Infof("[peermgr] passive mode, added %d/%d peers", len(added), len(m.peers))
	committed = true
	return nil
}
