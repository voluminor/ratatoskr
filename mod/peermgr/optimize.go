package peermgr

import (
	"context"
	"sync"
	"time"
)

// // // // // // // // // //

func (m *Obj) run(ctx context.Context) {
	_ = m.optimizeLocked(ctx)

	hasRefresh := m.cfg.RefreshInterval > 0
	hasWatch := m.cfg.MinPeers > 0

	if !hasRefresh && !hasWatch {
		return
	}

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

	if !hasRefresh {
		// No refresh timer — only react to watch signals
		for {
			select {
			case <-watchCh:
				_ = m.optimizeLocked(ctx)
			case <-ctx.Done():
				return
			}
		}
	}

	ticker := time.NewTicker(m.cfg.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = m.optimizeLocked(ctx)
		case <-watchCh:
			_ = m.optimizeLocked(ctx)
			ticker.Reset(m.cfg.RefreshInterval)
		case <-ctx.Done():
			return
		}
	}
}

// //

// watchPeers polls GetPeers at WatchInterval and sends on triggerCh
// when active Up count stays at or below MinPeers for
// MinPeersConfirmations consecutive ticks.
func (m *Obj) watchPeers(ctx context.Context, triggerCh chan<- struct{}) {
	threshold := int(m.cfg.MinPeers)
	confirmCount := 0

	ticker := time.NewTicker(WatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.mu.Lock()
			active := m.active
			m.mu.Unlock()

			up := countUpActive(active, m.node.GetPeers())

			if up <= threshold {
				confirmCount++
				m.cfg.Logger.Debugf("[peermgr] MinPeers watch: %d up <= %d threshold, confirmation %d/%d",
					up, threshold, confirmCount, MinPeersConfirmations)

				if confirmCount >= MinPeersConfirmations {
					m.cfg.Logger.Warnf("[peermgr] active peers (%d) at or below MinPeers (%d) for %d checks, triggering re-evaluation",
						up, threshold, MinPeersConfirmations)
					confirmCount = 0
					select {
					case triggerCh <- struct{}{}:
					default:
					}
				}
			} else {
				confirmCount = 0
			}
		case <-ctx.Done():
			return
		}
	}
}

// //

// optimizeLocked — serializes optimize calls via optimizeMu
func (m *Obj) optimizeLocked(ctx context.Context) error {
	m.optimizeMu.Lock()
	defer m.optimizeMu.Unlock()
	if m.cfg.MaxPerProto == -1 {
		return m.optimizePassive()
	}
	return m.optimizeActive(ctx)
}

// //

// optimizeActive — sliding race: adds in batches, drops losers after each
func (m *Obj) optimizeActive(ctx context.Context) error {
	peers := m.peers
	batchSize := m.cfg.BatchSize
	if batchSize <= 1 {
		batchSize = len(peers)
	}

	connected := make([]peerEntryObj, 0, len(peers))
	totalBatches := (len(peers) + batchSize - 1) / batchSize

	for i := 0; i < len(peers); i += batchSize {
		end := i + batchSize
		if end > len(peers) {
			end = len(peers)
		}
		batch := peers[i:end]
		batchIdx := i/batchSize + 1

		m.cfg.Logger.Debugf("[peermgr] batch %d/%d: adding %d candidates", batchIdx, totalBatches, len(batch))

		for _, p := range batch {
			if err := m.node.AddPeer(p.URI); err != nil {
				m.cfg.Logger.Debugf("[peermgr] AddPeer %s: %v", p.URI, err)
			}
		}
		connected = append(connected, batch...)

		m.cfg.Logger.Traceln("[peermgr] batch", batchIdx, "/", totalBatches, "waiting", m.cfg.ProbeTimeout, ",", len(connected), "connected")

		timer := time.NewTimer(m.cfg.ProbeTimeout)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}

		var err error
		connected, err = m.probeAndSelect(connected, batchIdx, totalBatches)
		if err != nil {
			return err
		}
	}

	m.reportResult(connected)
	return nil
}

// probeAndSelect — selects best peers, removes losers, updates m.active
func (m *Obj) probeAndSelect(connected []peerEntryObj, batchIdx, totalBatches int) ([]peerEntryObj, error) {
	results := buildResults(connected, m.node.GetPeers())
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
			m.cfg.Logger.Traceln("[peermgr] batch", batchIdx, "/", totalBatches, "removing loser", p.URI)
			if err := m.node.RemovePeer(p.URI); err != nil {
				m.cfg.Logger.Debugf("[peermgr] RemovePeer %s: %v", p.URI, err)
			}
		}
	}

	m.mu.Lock()
	m.active = uris
	m.mu.Unlock()

	return kept, nil
}

// reportResult logs the outcome; calls OnNoReachablePeers if result is empty
func (m *Obj) reportResult(connected []peerEntryObj) {
	if len(connected) == 0 {
		m.cfg.Logger.Warnf("[peermgr] no reachable peers after probe")
		if m.cfg.OnNoReachablePeers != nil {
			m.cfg.OnNoReachablePeers()
		}
	} else {
		uris := make([]string, len(connected))
		for i, p := range connected {
			uris[i] = p.URI
		}
		m.cfg.Logger.Infof("[peermgr] active peers: %v", uris)
	}
}

// //

// optimizePassive — mode -1: reconnects the entire peer list
func (m *Obj) optimizePassive() error {
	uris := make([]string, len(m.peers))
	for i, p := range m.peers {
		uris[i] = p.URI
		_ = m.node.RemovePeer(p.URI)
		if err := m.node.AddPeer(p.URI); err != nil {
			m.cfg.Logger.Debugf("[peermgr] AddPeer %s: %v", p.URI, err)
		}
	}

	m.mu.Lock()
	m.active = uris
	m.mu.Unlock()

	m.cfg.Logger.Infof("[peermgr] passive mode, added %d peers", len(m.peers))
	return nil
}
