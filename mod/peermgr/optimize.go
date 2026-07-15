package peermgr

import (
	"context"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

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

	if m.cfg.RefreshInterval <= 0 && (m.cfg.Passive || m.cfg.HealthInterval < 0) {
		return
	}
	var refreshTicker, healthTicker *time.Ticker
	var refreshC, healthC <-chan time.Time
	if m.cfg.RefreshInterval > 0 {
		refreshTicker = time.NewTicker(m.cfg.RefreshInterval)
		refreshC = refreshTicker.C
		defer refreshTicker.Stop()
	}
	if !m.cfg.Passive && m.cfg.HealthInterval > 0 {
		healthTicker = time.NewTicker(m.cfg.HealthInterval)
		healthC = healthTicker.C
		defer healthTicker.Stop()
	}
	confirmations := 0
	for {
		select {
		case <-refreshC:
			_ = m.optimizeLocked(ctx)
		case <-healthC:
			up := m.activeUpCount()
			if up == 0 {
				confirmations = 0
				_ = m.optimizeLockedMode(ctx, true)
				if refreshTicker != nil {
					refreshTicker.Reset(m.cfg.RefreshInterval)
				}
				continue
			}
			if m.cfg.MinPeers > 0 && up <= m.cfg.MinPeers {
				confirmations++
			} else {
				confirmations = 0
			}
			if confirmations >= m.cfg.MinPeersConfirmations {
				confirmations = 0
				_ = m.optimizeLockedMode(ctx, true)
				if refreshTicker != nil {
					refreshTicker.Reset(m.cfg.RefreshInterval)
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

func (m *Obj) optimizeLocked(ctx context.Context) error {
	return m.optimizeLockedMode(ctx, false)
}

func (m *Obj) optimizeLockedMode(ctx context.Context, recovery bool) error {
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
	return m.optimizeActiveMode(ctx, recovery)
}

func (m *Obj) activeUpCount() int {
	active := m.activeSet()
	up := make(map[string]bool, len(active))
	for _, peer := range m.cfg.Node.GetPeers() {
		key := normalizePeerURI(peer.URI)
		if _, managed := active[key]; managed && peer.Up {
			up[key] = true
		}
	}
	n := 0
	for key := range active {
		if up[key] {
			n++
		}
	}
	return n
}

// // // // // // // // // //

func (m *Obj) recoverySlots(active []PeerEntryObj, peers []yggcore.PeerInfo) (map[string]int, bool) {
	slots := make(map[string]int)
	for _, peer := range m.peers {
		slots[peer.Scheme] = m.cfg.MaxPerProto
	}
	activeKeys := make(map[string]string, len(active))
	for _, peer := range active {
		activeKeys[peerEntryKey(peer)] = peer.Scheme
	}
	present := make(map[string]struct{}, len(active))
	up := make(map[string]struct{}, len(active))
	for _, peer := range peers {
		key := normalizePeerURI(peer.URI)
		if _, managed := activeKeys[key]; !managed {
			continue
		}
		present[key] = struct{}{}
		if peer.Up {
			up[key] = struct{}{}
		}
	}
	for key, scheme := range activeKeys {
		if _, ok := up[key]; ok {
			slots[scheme]--
		}
	}
	if len(up) == 0 {
		return nil, true
	}
	for key, scheme := range activeKeys {
		if _, ok := present[key]; ok || slots[scheme] <= 0 {
			continue
		}
		slots[scheme]--
	}
	return slots, false
}

func (m *Obj) probeCycleCandidates(now time.Time, recovery bool, peerSnapshot []yggcore.PeerInfo) []PeerEntryObj {
	activeSet := m.activeSet()
	out := make([]PeerEntryObj, 0, len(activeSet)+effectiveBatchSize(m.cfg.BatchSize, len(m.peers)))
	active := make([]PeerEntryObj, 0, len(activeSet))
	seen := make(map[string]struct{}, len(m.peers))
	for _, peer := range m.peers {
		key := peerEntryKey(peer)
		if _, active := activeSet[key]; !active {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, peer)
		active = append(active, peer)
	}

	if len(m.peers) == 0 {
		return out
	}
	budget := effectiveBatchSize(m.cfg.BatchSize, len(m.peers))
	var slots map[string]int
	outage := false
	if recovery {
		slots, outage = m.recoverySlots(active, peerSnapshot)
	}
	start := m.probeCursor % len(m.peers)
	scanned := 0
	added := 0
	lastAdded := -1
	for scanned < len(m.peers) && added < budget {
		idx := (start + scanned) % len(m.peers)
		peer := m.peers[idx]
		scanned++
		key := peerEntryKey(peer)
		if _, ok := seen[key]; ok {
			continue
		}
		state := m.probeState[key]
		if !state.retryAfter.IsZero() && now.Before(state.retryAfter) {
			continue
		}
		if recovery {
			if !outage {
				if slots[peer.Scheme] <= 0 {
					continue
				}
				slots[peer.Scheme]--
			}
		} else if !state.holdoffUntil.IsZero() && now.Before(state.holdoffUntil) {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, peer)
		added++
		lastAdded = idx
	}
	if lastAdded >= 0 {
		m.probeCursor = (lastAdded + 1) % len(m.peers)
	} else {
		m.probeCursor = (start + scanned) % len(m.peers)
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

func peerEntryKey(peer PeerEntryObj) string {
	if peer.MatchURI != "" {
		return peer.MatchURI
	}
	return normalizePeerURI(peer.URI)
}

func (m *Obj) updateProbeSchedule(results []peerResultObj, selected map[string]bool, probeTimeout time.Duration) {
	now := time.Now()
	for _, r := range results {
		key := normalizePeerURI(r.URI)
		if selected[r.URI] {
			delete(m.probeState, key)
			continue
		}
		if r.Up {
			if m.cfg.ReprobeInterval < 0 {
				delete(m.probeState, key)
			} else {
				m.probeState[key] = probeStateObj{holdoffUntil: now.Add(m.cfg.ReprobeInterval)}
			}
			continue
		}
		m.bumpProbeBackoff(key, probeTimeout)
	}
}

func (m *Obj) bumpProbeBackoff(key string, probeTimeout time.Duration) {
	state := m.probeState[key]
	state.failures++
	state.holdoffUntil = time.Time{}
	delay := probeTimeout
	for i := 0; i < state.failures; i++ {
		delay *= 2
		if delay >= maxProbeBackoff {
			delay = maxProbeBackoff
			break
		}
	}
	state.retryAfter = time.Now().Add(delay)
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

func managedURIs(managed map[string]string) []string {
	out := make([]string, 0, len(managed))
	for _, uri := range managed {
		out = append(out, uri)
	}
	return out
}

// //

func (m *Obj) optimizeActiveMode(ctx context.Context, recovery bool) error {
	probeTimeout := m.cfg.ProbeTimeout
	peerSnapshot := m.cfg.Node.GetPeers()
	candidates := m.probeCycleCandidates(time.Now(), recovery, peerSnapshot)
	if len(candidates) == 0 {
		m.cfg.Logger.Debugf("[peermgr] no candidates due for probing")
		return nil
	}
	managed := make(map[string]string)
	m.mu.Lock()
	for _, uri := range m.active {
		managed[normalizePeerURI(uri)] = uri
	}
	m.mu.Unlock()

	if err := ctx.Err(); err != nil {
		m.setActive(managedURIs(managed))
		return err
	}
	candidateKeys := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidateKeys[peerEntryKey(candidate)] = struct{}{}
	}
	present := make(map[string]struct{}, len(candidates))
	for _, peer := range peerSnapshot {
		key := normalizePeerURI(peer.URI)
		if _, candidate := candidateKeys[key]; candidate {
			present[key] = struct{}{}
		}
	}
	connected := make([]PeerEntryObj, 0, len(candidates))
	startedConnection := false
	for _, p := range candidates {
		if err := ctx.Err(); err != nil {
			m.setActive(managedURIs(managed))
			return err
		}
		key := peerEntryKey(p)
		_, owned := managed[key]
		_, exists := present[key]
		if owned && exists {
			connected = append(connected, p)
			continue
		}
		if err := m.cfg.Node.AddPeer(p.URI); err != nil {
			m.cfg.Logger.Debugf("[peermgr] AddPeer %s: %v", normalizePeerURI(p.URI), err)
			if owned {
				connected = append(connected, p)
				continue
			}
			m.bumpProbeBackoff(key, probeTimeout)
			continue
		}
		managed[key] = p.URI
		connected = append(connected, p)
		startedConnection = true
	}

	if startedConnection {
		if err := waitProbeBatch(ctx, probeTimeout); err != nil {
			m.setActive(managedURIs(managed))
			return err
		}
	}
	kept := m.selectAndPrune(connected, probeTimeout, managed)

	keptURIs := make([]string, len(kept))
	for i, p := range kept {
		keptURIs[i] = p.URI
	}
	m.setActive(keptURIs)
	m.reportResult(kept)
	return nil
}

func (m *Obj) optimizePassive(ctx context.Context) error {
	managed := make(map[string]string, len(m.peers))
	m.mu.Lock()
	for _, uri := range m.active {
		managed[normalizePeerURI(uri)] = uri
	}
	m.mu.Unlock()
	present := make(map[string]struct{})
	for _, peer := range m.cfg.Node.GetPeers() {
		present[normalizePeerURI(peer.URI)] = struct{}{}
	}
	for _, peer := range m.peers {
		if err := ctx.Err(); err != nil {
			m.setActive(managedURIs(managed))
			return err
		}
		key := peerEntryKey(peer)
		if _, owned := managed[key]; owned {
			if _, exists := present[key]; exists {
				continue
			}
		}
		if err := m.cfg.Node.AddPeer(peer.URI); err != nil {
			m.cfg.Logger.Debugf("[peermgr] AddPeer %s: %v", normalizePeerURI(peer.URI), err)
			continue
		}
		managed[key] = peer.URI
	}
	m.setActive(managedURIs(managed))
	m.cfg.Logger.Infof("[peermgr] passive mode, managing %d peers", len(managed))
	return nil
}

func (m *Obj) selectAndPrune(connected []PeerEntryObj, probeTimeout time.Duration, managed map[string]string) []PeerEntryObj {
	results := buildResults(connected, m.cfg.Node.GetPeers())
	selected := selectBest(results, m.cfg.MaxPerProto)
	selectedSet := make(map[string]bool, len(selected))
	for _, r := range selected {
		selectedSet[r.URI] = true
	}
	m.updateProbeSchedule(results, selectedSet, probeTimeout)
	m.cfg.Logger.Debugf("[peermgr] %d up, %d selected, %d dropped",
		countUp(results), len(selected), len(connected)-len(selected))

	kept := make([]PeerEntryObj, 0, len(selected))
	for _, p := range connected {
		if selectedSet[p.URI] {
			kept = append(kept, p)
			continue
		}
		if err := m.cfg.Node.RemovePeer(p.URI); err != nil {
			m.cfg.Logger.Debugf("[peermgr] RemovePeer %s: %v", normalizePeerURI(p.URI), err)
			kept = append(kept, p)
			continue
		}
		delete(managed, peerEntryKey(p))
	}
	return kept
}

// //

func (m *Obj) reportResult(kept []PeerEntryObj) {
	up := countUp(buildResults(kept, m.cfg.Node.GetPeers()))
	if up == 0 {
		m.cfg.Logger.Warnf("[peermgr] no reachable peers after probe")
		if m.cfg.NoReachablePeers != nil {
			select {
			case m.cfg.NoReachablePeers <- struct{}{}:
			default:
			}
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
