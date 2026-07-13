package peermgr

import (
	"context"
	"time"
)

// // // // // // // // // //

// lockOptimize acquires the single-flight optimize gate, honoring ctx cancellation.
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

// run drives scheduled re-evaluation: one optimize at startup, then one every
// RefreshInterval (0 disables the ticker, leaving only the startup pass).
func (m *Obj) run(ctx context.Context) {
	_ = m.optimizeLocked(ctx)

	if m.cfg.RefreshInterval <= 0 && m.cfg.MinPeers <= 0 {
		return
	}
	var refreshTicker, watchTicker *time.Ticker
	var refreshC, watchC <-chan time.Time
	if m.cfg.RefreshInterval > 0 {
		refreshTicker = time.NewTicker(m.cfg.RefreshInterval)
		refreshC = refreshTicker.C
		defer refreshTicker.Stop()
	}
	if m.cfg.MinPeers > 0 {
		watchTicker = time.NewTicker(m.cfg.WatchInterval)
		watchC = watchTicker.C
		defer watchTicker.Stop()
	}
	confirmations := 0
	for {
		select {
		case <-refreshC:
			_ = m.optimizeLocked(ctx)
		case <-watchC:
			if m.activeUpCount() <= m.cfg.MinPeers {
				confirmations++
			} else {
				confirmations = 0
			}
			if confirmations >= m.cfg.MinPeersConfirmations {
				confirmations = 0
				_ = m.optimizeLocked(ctx)
				if refreshTicker != nil {
					refreshTicker.Reset(m.cfg.RefreshInterval)
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

// optimizeLocked serializes optimize calls through the gate and runs one cycle.
func (m *Obj) optimizeLocked(ctx context.Context) error {
	if err := m.lockOptimize(ctx); err != nil {
		return err
	}
	defer m.unlockOptimize()
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.cfg.Passive {
		return m.optimizePassive(ctx)
	}
	return m.optimizeActive(ctx)
}

func (m *Obj) activeUpCount() int {
	active := m.activeSet()
	n := 0
	for _, peer := range m.node.GetPeers() {
		if _, ok := active[normalizePeerURI(peer.URI)]; ok && peer.Up {
			n++
		}
	}
	return n
}

// // // // // // // // // //

// probeCandidates returns the peers due for probing: everything currently active
// plus any peer whose backoff window has elapsed.
func (m *Obj) probeCandidates(now time.Time) []peerEntryObj {
	activeSet := m.activeSet()
	out := make([]peerEntryObj, 0, len(m.peers))
	seen := make(map[string]struct{}, len(m.peers))
	for _, peer := range m.peers {
		key := peerEntryKey(peer)
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

// updateProbeBackoff clears state for reachable peers and grows an exponential
// backoff (capped) for those still down, so dead URIs are re-probed less often.
func (m *Obj) updateProbeBackoff(results []peerResultObj, probeTimeout time.Duration) {
	for _, r := range results {
		key := normalizePeerURI(r.URI)
		if r.Up {
			delete(m.probeState, key)
			continue
		}
		m.bumpProbeBackoff(key, probeTimeout)
	}
}

// bumpProbeBackoff grows the capped exponential backoff for one peer key. It is
// used both for peers that connect but fail the probe and for peers that never
// reach the probe stage (AddPeer fails synchronously on a malformed URI), so an
// unusable URI decays out of the candidate set instead of being re-dialed every
// cycle. Called only from the single optimize goroutine, so probeState is unlocked.
func (m *Obj) bumpProbeBackoff(key string, probeTimeout time.Duration) {
	state := m.probeState[key]
	state.failures++
	delay := probeTimeout
	for i := 0; i < state.failures; i++ {
		delay *= 2
		if delay >= maxProbeBackoff {
			delay = maxProbeBackoff
			break
		}
	}
	state.nextTry = time.Now().Add(delay)
	m.probeState[key] = state
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

// effectiveWindow bounds how many peers are probed simultaneously in one window.
func effectiveWindow(size, total int) int {
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

// managedURIs returns the URIs currently handed to the node, order-independent.
func managedURIs(managed map[string]string) []string {
	out := make([]string, 0, len(managed))
	for _, uri := range managed {
		out = append(out, uri)
	}
	return out
}

// //

// optimizeActive probes candidates in windows of at most `window` peers, giving
// each window a single ProbeTimeout before selecting the best per protocol.
// Winners carry into later windows so the selection is global, while the window
// bounds how many peers are connected at once. A peer list that fits one window
// costs a single ProbeTimeout, not one per batch.
func (m *Obj) optimizeActive(ctx context.Context) error {
	probeTimeout := m.cfg.ProbeTimeout
	candidates := m.probeCandidates(time.Now())
	if len(candidates) == 0 {
		m.cfg.Logger.Debugf("[peermgr] no candidates due for probing")
		return nil
	}
	window := effectiveWindow(m.cfg.BatchSize, len(candidates))

	// managed maps normalized key → URI for every peer currently handed to the
	// node. It is seeded with the already-active peers so a cancelled cycle still
	// reports them (plus anything added since, minus losers already removed) for
	// Stop to reap — losing them here would leak established peers past Stop.
	managed := make(map[string]string)
	m.mu.Lock()
	for _, uri := range m.active {
		managed[normalizePeerURI(uri)] = uri
	}
	m.mu.Unlock()

	// kept holds the current winners (already connected); they re-enter each
	// window's measurement so the final selection is global.
	var kept []peerEntryObj

	for start := 0; start < len(candidates); start += window {
		if err := ctx.Err(); err != nil {
			m.setActive(managedURIs(managed))
			return err
		}
		end := start + window
		if end > len(candidates) {
			end = len(candidates)
		}

		connected := append([]peerEntryObj(nil), kept...)
		for _, p := range candidates[start:end] {
			key := peerEntryKey(p)
			if _, ok := managed[key]; ok {
				connected = append(connected, p)
				continue
			}
			if err := m.node.AddPeer(p.URI); err != nil {
				m.cfg.Logger.Debugf("[peermgr] AddPeer %s: %v", normalizePeerURI(p.URI), err)
				m.bumpProbeBackoff(key, probeTimeout)
				continue
			}
			managed[key] = p.URI
			connected = append(connected, p)
		}

		if err := waitProbeBatch(ctx, probeTimeout); err != nil {
			m.setActive(managedURIs(managed))
			return err
		}
		kept = m.selectAndPrune(connected, probeTimeout, managed)
	}

	keptURIs := make([]string, len(kept))
	for i, p := range kept {
		keptURIs[i] = p.URI
	}
	m.setActive(keptURIs)
	m.reportResult(kept)
	return nil
}

// optimizePassive keeps the complete configured set managed without probing or
// pruning. AddPeer is retried on every cycle so externally dropped peers recover.
func (m *Obj) optimizePassive(ctx context.Context) error {
	managed := make(map[string]string, len(m.peers))
	m.mu.Lock()
	for _, uri := range m.active {
		managed[normalizePeerURI(uri)] = uri
	}
	m.mu.Unlock()
	for _, peer := range m.peers {
		if err := ctx.Err(); err != nil {
			m.setActive(managedURIs(managed))
			return err
		}
		if err := m.node.AddPeer(peer.URI); err != nil {
			m.cfg.Logger.Debugf("[peermgr] AddPeer %s: %v", normalizePeerURI(peer.URI), err)
			continue
		}
		managed[peerEntryKey(peer)] = peer.URI
	}
	m.setActive(managedURIs(managed))
	m.cfg.Logger.Infof("[peermgr] passive mode, managing %d peers", len(managed))
	return nil
}

// selectAndPrune measures the connected candidates against GetPeers, keeps the
// best per protocol, and removes the losers from the node (and from managed).
func (m *Obj) selectAndPrune(connected []peerEntryObj, probeTimeout time.Duration, managed map[string]string) []peerEntryObj {
	results := buildResults(connected, m.node.GetPeers())
	m.updateProbeBackoff(results, probeTimeout)
	selected := selectBest(results, m.cfg.MaxPerProto)
	selectedSet := make(map[string]bool, len(selected))
	for _, r := range selected {
		selectedSet[r.URI] = true
	}
	m.cfg.Logger.Debugf("[peermgr] %d up, %d selected, %d dropped",
		countUp(results), len(selected), len(connected)-len(selected))

	kept := make([]peerEntryObj, 0, len(selected))
	for _, p := range connected {
		if selectedSet[p.URI] {
			kept = append(kept, p)
			continue
		}
		if err := m.node.RemovePeer(p.URI); err != nil {
			m.cfg.Logger.Debugf("[peermgr] RemovePeer %s: %v", normalizePeerURI(p.URI), err)
			// The peer is still handed to the node. Keep it managed and retry the
			// reconciliation on the next cycle instead of losing teardown ownership.
			kept = append(kept, p)
			continue
		}
		delete(managed, peerEntryKey(p))
	}
	return kept
}

// //

// reportResult logs the optimize outcome.
func (m *Obj) reportResult(kept []peerEntryObj) {
	up := countUp(buildResults(kept, m.node.GetPeers()))
	if up == 0 {
		m.cfg.Logger.Warnf("[peermgr] no reachable peers after probe")
		if m.cfg.OnNoReachablePeers != nil {
			m.cfg.OnNoReachablePeers()
		}
		return
	}
	m.cfg.Logger.Infof("[peermgr] %d active peers", up)
}

func (m *Obj) setActive(active []string) {
	m.mu.Lock()
	m.active = append([]string(nil), active...)
	m.mu.Unlock()
}
