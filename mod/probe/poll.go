package probe

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"
)

// // // // // // // // // //

const (
	defaultPollInterval     = 200 * time.Millisecond
	defaultLookupRetryEvery = time.Second
	defaultHopsWaitTimeout  = 2 * time.Second
)

// // // // // // // // // //

// Trace searches for the key in both spanning tree and pathfinder.
// Returns immediately if both found; waits for hops if only tree is available;
// falls back to full poll with lookup retries until ctx expires.
func (o *Obj) Trace(ctx context.Context, key ed25519.PublicKey) (*TraceResultObj, error) {
	if o.isClosed() {
		return nil, ErrClosed
	}
	if err := validateKey(key); err != nil {
		return nil, err
	}
	var cancel context.CancelFunc
	ctx, cancel = o.boundedContext(ctx)
	defer cancel()
	result := o.collect(key)

	if result != nil && result.TreePath != nil && result.Hops != nil {
		o.enrichPath(ctx, result.TreePath)
		return result, nil
	}

	o.Lookup(key)

	if result != nil {
		if result.TreePath != nil && result.Hops == nil {
			result, _ = o.pollFull(ctx, key, result)
		}
		o.enrichPath(ctx, result.TreePath)
		return result, nil
	}

	o.logger.Infof("[probe] lookup started for %x", key[:8])
	result, err := o.pollFull(ctx, key, nil)
	if result != nil && result.TreePath != nil {
		o.enrichPath(ctx, result.TreePath)
	}
	return result, err
}

// //

// pollFull polls for both tree path and hops until ctx expires.
// Once tree is found, waits up to HopsWaitTimeout for hops before returning.
func (o *Obj) pollFull(ctx context.Context, key ed25519.PublicKey, initial *TraceResultObj) (*TraceResultObj, error) {
	ticker := time.NewTicker(o.pollInterval)
	defer ticker.Stop()

	lastLookup := time.Now()
	result := initial
	var hopsDeadline time.Time
	if result != nil && result.TreePath != nil && result.Hops == nil {
		hopsDeadline = time.Now().Add(defaultHopsWaitTimeout)
	}

	for {
		select {
		case <-ctx.Done():
			if latest := o.collect(key); latest != nil {
				result = latest
			}
			if result != nil {
				return result, fmt.Errorf("%w: %w", ErrLookupTimedOut, ctx.Err())
			}
			return nil, fmt.Errorf("%w for key %x: %w", ErrLookupTimedOut, key[:8], ctx.Err())
		case <-ticker.C:
			now := time.Now()
			if latest := o.collect(key); latest != nil {
				result = latest
			}

			if result != nil && result.TreePath != nil && result.Hops != nil {
				return result, nil
			}

			if result != nil && result.TreePath != nil && result.Hops == nil {
				if hopsDeadline.IsZero() {
					hopsDeadline = now.Add(defaultHopsWaitTimeout)
					o.Lookup(key)
					lastLookup = now
				}
				if !now.Before(hopsDeadline) {
					return result, nil
				}
			}

			if now.Sub(lastLookup) >= o.lookupRetryEvery {
				o.Lookup(key)
				lastLookup = now
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
	for _, p := range o.source.GetPeers() {
		if p.Up && len(p.Key) == ed25519.PublicKeySize && p.Latency > 0 {
			peerLatency[toKeyArray(p.Key)] = p.Latency
		}
	}

	targets := path[1:]
	type rttResultObj struct {
		idx int
		rtt time.Duration
	}

	// Direct peers get RTT from core; the rest are queried remotely. Collect the
	// remote indices first so the result channel can buffer every job; workers can
	// finish without blocking even if ctx cancels the wait.
	var remote []int
	for i, n := range targets {
		if lat, ok := peerLatency[toKeyArray(n.Key)]; ok {
			targets[i].RTT = lat
			continue
		}
		if n.RTT == 0 {
			remote = append(remote, i)
		}
	}
	if len(remote) == 0 {
		return
	}

	ch := make(chan rttResultObj, len(remote))
	jobs := make(chan int, len(remote))
	for _, idx := range remote {
		jobs <- idx
	}
	close(jobs)

	workerCount := len(remote)
	if workerCount > DefaultMaxConcurrency {
		workerCount = DefaultMaxConcurrency
	}
	for range workerCount {
		go func() {
			for idx := range jobs {
				_, rtt, _ := o.callRemotePeers(ctx, targets[idx].Key)
				ch <- rttResultObj{idx: idx, rtt: rtt}
			}
		}()
	}
	for range remote {
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
