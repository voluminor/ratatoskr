package peermgr

import (
	"context"
	"time"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
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

// run drives one bounded startup pass, scheduled refreshes, and health recovery.
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

// optimizeLocked serializes optimize calls through the gate and runs one cycle.
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

// recoverySlots returns the remaining per-protocol capacity for partial
// recovery. Missing active peers reserve a slot because the cycle reconnects
// them itself; present-but-down peers do not.
func (m *Obj) recoverySlots(active []peerEntryObj, peers []yggcore.PeerInfo) (map[string]int, bool) {
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

// probeCycleCandidates returns every currently active peer plus one bounded
// batch of challengers. A partial recovery targets only vacant protocol slots;
// a complete outage bypasses holdoff for the whole bounded batch.
func (m *Obj) probeCycleCandidates(now time.Time, recovery bool, peerSnapshot []yggcore.PeerInfo) []peerEntryObj {
	activeSet := m.activeSet()
	out := make([]peerEntryObj, 0, len(activeSet)+effectiveBatchSize(m.cfg.BatchSize, len(m.peers)))
	active := make([]peerEntryObj, 0, len(activeSet))
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

func peerEntryKey(peer peerEntryObj) string {
	if peer.MatchURI != "" {
		return peer.MatchURI
	}
	return normalizePeerURI(peer.URI)
}

// updateProbeSchedule clears state for selected peers, holds reachable losers for
// ReprobeInterval, and grows exponential backoff for peers still down.
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

// bumpProbeBackoff grows the capped exponential backoff for one peer key. It is
// used both for peers that connect but fail the probe and for peers that never
// reach the probe stage (AddPeer fails synchronously on a malformed URI), so an
// unusable URI decays out of the candidate set instead of being re-dialed every
// cycle. Called only from the single optimize goroutine, so probeState is unlocked.
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

// effectiveBatchSize bounds new candidate connection attempts in one cycle.
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

// managedURIs returns the URIs currently handed to the node, order-independent.
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
	// managed maps normalized key → URI for every peer currently handed to the
	// node. It is seeded with the already-active peers so a cancelled cycle still
	// reports them (plus anything added since, minus losers already removed) for
	// Close to reap — losing them here would leak established peers past Close.
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
	connected := make([]peerEntryObj, 0, len(candidates))
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
				// GetPeers and AddPeer are not atomic. Preserve teardown ownership
				// until the fresh selection snapshot confirms the peer is absent and
				// RemovePeer succeeds.
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

// optimizePassive keeps the complete configured set managed without probing or
// pruning. AddPeer is retried on every cycle so externally dropped peers recover.
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
			// A stale snapshot cannot revoke ownership. Close or a later
			// reconciliation remains responsible for an already-owned peer.
			continue
		}
		managed[key] = peer.URI
	}
	m.setActive(managedURIs(managed))
	m.cfg.Logger.Infof("[peermgr] passive mode, managing %d peers", len(managed))
	return nil
}

// selectAndPrune measures the connected candidates against GetPeers, keeps the
// best per protocol, and removes the losers from the node (and from managed).
func (m *Obj) selectAndPrune(connected []peerEntryObj, probeTimeout time.Duration, managed map[string]string) []peerEntryObj {
	results := buildResults(connected, m.cfg.Node.GetPeers())
	selected := selectBest(results, m.cfg.MaxPerProto)
	selectedSet := make(map[string]bool, len(selected))
	for _, r := range selected {
		selectedSet[r.URI] = true
	}
	m.updateProbeSchedule(results, selectedSet, probeTimeout)
	m.cfg.Logger.Debugf("[peermgr] %d up, %d selected, %d dropped",
		countUp(results), len(selected), len(connected)-len(selected))

	kept := make([]peerEntryObj, 0, len(selected))
	for _, p := range connected {
		if selectedSet[p.URI] {
			kept = append(kept, p)
			continue
		}
		if err := m.cfg.Node.RemovePeer(p.URI); err != nil {
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
