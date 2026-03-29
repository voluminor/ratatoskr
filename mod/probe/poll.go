package probe

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"
)

// // // // // // // // // //

var (
	// PollInterval controls how often Trace polls core for results.
	PollInterval = 200 * time.Millisecond

	// LookupRetryEvery controls how often SendLookup is retried during polling.
	LookupRetryEvery = time.Second

	// HopsWaitTimeout is how long Trace waits for hops when tree path is already found.
	HopsWaitTimeout = 2 * time.Second
)

const hopsGracePeriod = 10

// // // // // // // // // //

// Trace searches for the key in both spanning tree and pathfinder.
// Returns immediately if both found; waits for hops if only tree is available;
// falls back to full poll with lookup retries until ctx expires.
func (o *Obj) Trace(ctx context.Context, key ed25519.PublicKey) (*TraceResultObj, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	result := o.collect(key)

	if result != nil && result.TreePath != nil && result.Hops != nil {
		o.enrichPath(ctx, result.TreePath)
		return result, nil
	}

	o.Lookup(key)

	if result != nil {
		if result.Hops == nil {
			enriched := o.pollHops(ctx, key, HopsWaitTimeout)
			if enriched != nil {
				result.Hops = enriched
			}
		}
		o.enrichPath(ctx, result.TreePath)
		return result, nil
	}

	o.logger.Infof("[probe] lookup started for %x", key[:8])
	result, err := o.pollFull(ctx, key)
	if result != nil && result.TreePath != nil {
		o.enrichPath(ctx, result.TreePath)
	}
	return result, err
}

// //

// pollHops waits for hops to appear within maxWait. One retry lookup after ~1s.
func (o *Obj) pollHops(ctx context.Context, key ed25519.PublicKey, maxWait time.Duration) []HopObj {
	startTime := time.Now()
	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()

	retried := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if time.Since(startTime) > maxWait {
				hops, _ := o.Hops(key)
				return hops
			}
			if hops, err := o.Hops(key); err == nil {
				return hops
			}
			if !retried && time.Since(startTime) > time.Second {
				o.Lookup(key)
				retried = true
			}
		}
	}
}

// //

// pollFull polls for both tree path and hops until ctx expires.
// Once tree is found, gives hopsGracePeriod extra ticks before returning.
func (o *Obj) pollFull(ctx context.Context, key ed25519.PublicKey) (*TraceResultObj, error) {
	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()

	lastLookup := time.Now()
	graceTicks := -1

	for {
		select {
		case <-ctx.Done():
			if result := o.collect(key); result != nil {
				return result, fmt.Errorf("%w: %w", ErrLookupTimedOut, ctx.Err())
			}
			return nil, fmt.Errorf("%w for key %x: %w", ErrLookupTimedOut, key[:8], ctx.Err())
		case <-ticker.C:
			result := o.collect(key)

			if result != nil && result.TreePath != nil && result.Hops != nil {
				return result, nil
			}

			if result != nil && result.TreePath != nil && graceTicks < 0 {
				graceTicks = hopsGracePeriod
				o.Lookup(key)
				lastLookup = time.Now()
			}

			if graceTicks > 0 {
				graceTicks--
			} else if graceTicks == 0 {
				if result != nil {
					return result, nil
				}
				graceTicks = -1
			}

			if time.Since(lastLookup) >= LookupRetryEvery {
				o.Lookup(key)
				lastLookup = time.Now()
			}
		}
	}
}

// //

// enrichPath fills RTT on path nodes (skips root).
// Direct peers use core's latency; remote nodes use callRemotePeers round-trip.
func (o *Obj) enrichPath(ctx context.Context, path []*NodeObj) {
	if len(path) <= 1 {
		return
	}

	peerLatency := make(map[[ed25519.PublicKeySize]byte]time.Duration)
	for _, p := range o.core.GetPeers() {
		if p.Up && len(p.Key) == ed25519.PublicKeySize && p.Latency > 0 {
			peerLatency[toKeyArray(p.Key)] = p.Latency
		}
	}

	targets := path[1:]
	type rttResultObj struct {
		idx int
		rtt time.Duration
	}

	var remoteCount int
	for i, n := range targets {
		if lat, ok := peerLatency[toKeyArray(n.Key)]; ok {
			targets[i].RTT = lat
		} else {
			remoteCount++
		}
	}
	if remoteCount == 0 {
		return
	}

	ch := make(chan rttResultObj, remoteCount)
	for i, n := range targets {
		if n.RTT > 0 {
			continue
		}
		go func(idx int, k ed25519.PublicKey) {
			_, rtt, _ := o.callRemotePeers(ctx, k)
			ch <- rttResultObj{idx: idx, rtt: rtt}
		}(i, n.Key)
	}
	for range remoteCount {
		select {
		case r := <-ch:
			targets[r.idx].RTT = r.rtt
		case <-ctx.Done():
			return
		}
	}
}

// collect attempts to gather data from both tree and pathfinder sources.
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
