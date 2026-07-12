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

	if m.cfg.RefreshInterval <= 0 {
		return
	}
	ticker := time.NewTicker(m.cfg.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = m.optimizeLocked(ctx)
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
	return m.optimizeActive(ctx)
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
	m.reportResult(keptURIs)
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
		}
		delete(managed, peerEntryKey(p))
	}
	return kept
}

// //

// reportResult logs the optimize outcome.
func (m *Obj) reportResult(keptURIs []string) {
	if len(keptURIs) == 0 {
		m.cfg.Logger.Warnf("[peermgr] no reachable peers after probe")
		return
	}
	m.cfg.Logger.Infof("[peermgr] %d active peers", len(keptURIs))
}

func (m *Obj) setActive(active []string) {
	m.mu.Lock()
	m.active = append([]string(nil), active...)
	m.mu.Unlock()
}
