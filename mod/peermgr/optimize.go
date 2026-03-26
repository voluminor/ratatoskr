package peermgr

import (
	"context"
	"fmt"
	"time"
)

// // // // // // // // // //

func (m *Obj) run(ctx context.Context) {
	_ = m.optimize(ctx)

	if m.cfg.RefreshInterval <= 0 {
		return
	}
	ticker := time.NewTicker(m.cfg.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = m.optimize(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (m *Obj) optimize(ctx context.Context) error {
	if m.cfg.MaxPerProto == -1 {
		return m.optimizePassive()
	}
	return m.optimizeActive(ctx)
}

// //

// optimizeActive — скользящая гонка: батчами добавляет, после каждого отсекает худших
func (m *Obj) optimizeActive(ctx context.Context) error {
	peers := m.cfg.Peers
	batchSize := m.cfg.BatchSize
	if batchSize <= 1 {
		batchSize = len(peers)
	}

	connected := make([]string, 0, len(peers))
	totalBatches := (len(peers) + batchSize - 1) / batchSize

	for i := 0; i < len(peers); i += batchSize {
		end := i + batchSize
		if end > len(peers) {
			end = len(peers)
		}
		batch := peers[i:end]
		batchIdx := i/batchSize + 1

		m.cfg.Logger.Debugf("[peermgr] batch %d/%d: adding %d candidates %v", batchIdx, totalBatches, len(batch), batch)

		for _, uri := range batch {
			if err := m.node.AddPeer(uri); err != nil {
				m.cfg.Logger.Debugf("[peermgr] AddPeer %s: %v", uri, err)
			}
		}
		connected = append(connected, batch...)

		m.cfg.Logger.Traceln(fmt.Sprintf("[peermgr] batch %d/%d: waiting %s, %d connected", batchIdx, totalBatches, m.cfg.ProbeTimeout, len(connected)))

		select {
		case <-time.After(m.cfg.ProbeTimeout):
		case <-ctx.Done():
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

// probeAndSelect — выбирает лучших, удаляет проигравших, обновляет m.active
func (m *Obj) probeAndSelect(connected []string, batchIdx, totalBatches int) ([]string, error) {
	results := buildResults(connected, m.node.GetPeers())
	selected := selectBest(results, m.cfg.MaxPerProto)

	m.cfg.Logger.Debugf("[peermgr] batch %d/%d: %d up, %d selected, %d dropped",
		batchIdx, totalBatches, countUp(results), len(selected), len(connected)-len(selected))

	selectedSet := make(map[string]bool, len(selected))
	for _, uri := range selected {
		selectedSet[uri] = true
	}
	for _, uri := range connected {
		if !selectedSet[uri] {
			m.cfg.Logger.Traceln(fmt.Sprintf("[peermgr] batch %d/%d: removing loser %s", batchIdx, totalBatches, uri))
			if err := m.node.RemovePeer(uri); err != nil {
				m.cfg.Logger.Debugf("[peermgr] RemovePeer %s: %v", uri, err)
			}
		}
	}

	m.mu.Lock()
	m.active = selected
	m.mu.Unlock()

	return selected, nil
}

// reportResult логирует итог; вызывает OnNoReachablePeers при пустом результате
func (m *Obj) reportResult(connected []string) {
	if len(connected) == 0 {
		m.cfg.Logger.Warnf("[peermgr] no reachable peers after probe")
		if m.cfg.OnNoReachablePeers != nil {
			m.cfg.OnNoReachablePeers()
		}
	} else {
		m.cfg.Logger.Infof("[peermgr] active peers: %v", connected)
	}
}

// //

// optimizePassive — режим -1: переподключает весь список целиком
func (m *Obj) optimizePassive() error {
	for _, uri := range m.cfg.Peers {
		_ = m.node.RemovePeer(uri)
		if err := m.node.AddPeer(uri); err != nil {
			m.cfg.Logger.Debugf("[peermgr] AddPeer %s: %v", uri, err)
		}
	}

	m.mu.Lock()
	m.active = append([]string(nil), m.cfg.Peers...)
	m.mu.Unlock()

	m.cfg.Logger.Infof("[peermgr] passive mode, added %d peers", len(m.cfg.Peers))
	return nil
}
